package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/projector/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const serviceName = "projector"

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	if err != nil {
		log.Fatal("failed to start new relic", zap.Error(err))
	}
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	valkeyClient, err := pkg_valkey.NewClient(
		env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		env.Get("VALKEY_PASSWORD", ""),
	)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()
	valkeyStore := projection.NewStore(valkeyClient)

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	rpcURL := bus.RPCURL(natsURL)

	if err := bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	// One durable group for the whole projector fleet: each event is folded
	// into Valkey exactly once, and the durable consumer keeps its position
	// across restarts.
	sub, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect subscriber", zap.Error(err))
	}
	defer func() { _ = sub.Close() }()

	nc, err := bus.Connect(rpcURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect nats", zap.Error(err))
	}
	defer nc.Close()

	// JetStream publisher for escalating a cold live query onto the outgress
	// system lane (the live RPC's Twitch re-check).
	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	// Core-NATS subject for fanning tier-cache invalidations to every projector
	// pod (see Projector.broadcastInvalidate / rpc.SubscribeStatus).
	invalidateSubject := env.Get("NATS_PROJECTOR_TIER_INVALIDATE_SUBJECT", "bagel.internal.projector.tier.invalidate")

	// Core-NATS prefix for fanning section-scoped cache invalidations
	// (commands, modules) to the console cache bus after Valkey is updated.
	cacheInvalidatePrefix := env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate")

	// Stream Online Pre-Warming subjects: resolved here so Prewarmer owns them.
	streamTopic := env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream")
	usersTopic := env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get")
	modulesTopic := env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get")
	commandsTopic := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get")

	prewarmer := NewPrewarmer(valkeyStore, nc, usersTopic, modulesTopic, commandsTopic, log)
	projector := NewProjector(valkeyStore, nc, invalidateSubject, cacheInvalidatePrefix, prewarmer, log)

	if err := bus.Consume(ctx, nrApp, sub, data.SubjectUserChanged, projector.HandleUserChanged, log); err != nil {
		log.Fatal("failed to subscribe to user changes", zap.Error(err))
	}

	if err := bus.Consume(ctx, nrApp, sub, data.SubjectUserDeleted, projector.HandleUserDeleted, log); err != nil {
		log.Fatal("failed to subscribe to user deletions", zap.Error(err))
	}

	if err := bus.Consume(ctx, nrApp, sub, data.SubjectModuleChanged, projector.HandleModuleChanged, log); err != nil {
		log.Fatal("failed to subscribe to module changes", zap.Error(err))
	}

	if err := bus.Consume(ctx, nrApp, sub, data.SubjectCommandChanged, projector.HandleCommandChanged, log); err != nil {
		log.Fatal("failed to subscribe to command changes", zap.Error(err))
	}

	// Durable JetStream consumer, not a plain core Subscribe: a stream-online
	// only writes shared Valkey state and pre-warms the shared projection, so
	// exactly one projector pod must handle each event. The durable is keyed by
	// the projector's service group (sub), so projector pods share one consumer
	// (one prewarm per event, not pods x 3 prewarm RPCs) and the consumer
	// survives restarts. Other subsystems on this subject bind their own durable
	// under their own group and still get every event once.
	if err := bus.Consume(ctx, nrApp, sub, streamTopic, projector.HandleStreamEvent, log); err != nil {
		log.Fatal("failed to subscribe to stream online events", zap.Error(err))
	}

	subject := env.Get("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get")
	if err := rpc.SubscribeStatus(nc, valkeyStore, subject, usersTopic, invalidateSubject, "projector-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe status rpc", zap.Error(err))
	}

	dashboardSubject := env.Get("NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.projector.dashboard")
	if err := rpc.SubscribeDashboard(nc, valkeyStore, dashboardSubject, commandsTopic, modulesTopic, "projector-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe dashboard projector rpc", zap.Error(err))
	}

	// Live verb: the worker asks here when its live key is cold; the projector
	// answers from its projection or escalates to Twitch via the outgress system
	// lane.
	liveSubject := env.Get("NATS_BROADCASTER_LIVE_SUBJECT", "bagel.rpc.broadcaster.live.get")
	outgressSystemSubject := env.Get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system")
	if err := rpc.SubscribeLive(nc, valkeyStore, pub, liveSubject, outgressSystemSubject, "projector-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe live rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("projector ready",
		zap.String("status_subject", subject),
		zap.String("dashboard_subject", dashboardSubject),
		zap.String("stream_subject", streamTopic))

	<-ctx.Done()

	log.Info("projector shutting down")
}
