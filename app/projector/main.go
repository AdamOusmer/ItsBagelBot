package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/projector/hydration"
	"ItsBagelBot/app/projector/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const serviceName = "projector"

// fatalIf aborts startup on err: the projector cannot run degraded without any
// of its core dependencies, so a failed step must crash the pod.
func fatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

// projectorTopics are the projection RPC / hydration subjects read from the
// environment once at startup.
type projectorTopics struct {
	stream            string
	users             string
	modules           string
	commands          string
	invalidate        string
	cacheInvalidate   string
	status            string
	dashboard         string
	live              string
	outgressSystem    string
	hydrationConcurr  int
	queryHydrationTTL time.Duration
	liveHydrationTTL  time.Duration
}

func loadTopics() projectorTopics {
	return projectorTopics{
		stream:            env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
		users:             env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		modules:           env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get"),
		commands:          env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get"),
		invalidate:        env.Get("NATS_PROJECTOR_TIER_INVALIDATE_SUBJECT", "bagel.internal.projector.tier.invalidate"),
		cacheInvalidate:   env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),
		status:            env.Get("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
		dashboard:         env.Get("NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.projector.dashboard"),
		live:              env.Get("NATS_BROADCASTER_LIVE_SUBJECT", "bagel.rpc.broadcaster.live.get"),
		outgressSystem:    env.Get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"),
		hydrationConcurr:  env.GetInt("PROJECTOR_HYDRATION_CONCURRENCY", 8),
		queryHydrationTTL: env.GetDuration("PROJECTOR_QUERY_HYDRATION_TTL", 2*time.Hour),
		liveHydrationTTL:  env.GetDuration("PROJECTOR_LIVE_HYDRATION_TTL", projection.DefaultTTL),
	}
}

func main() {
	validate.CheckFloor = moderation.CheckFloor

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	fatalIf(log, err, "failed to start new relic")
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	valkeyClient, err := pkg_valkey.NewClient(
		env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		env.Get("VALKEY_PASSWORD", ""),
	)
	fatalIf(log, err, "failed to connect to valkey")
	defer valkeyClient.Close()
	valkeyStore := projection.NewStore(valkeyClient)

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	nc, pub, sub := connectBus(ctx, natsURL, log)
	defer nc.Close()
	defer func() { _ = pub.Close() }()
	defer func() { _ = sub.Close() }()

	topics := loadTopics()
	hydrator := hydration.New(valkeyStore, nc, projection.Subjects{
		Users:    topics.users,
		Modules:  topics.modules,
		Commands: topics.commands,
	}, topics.queryHydrationTTL, topics.liveHydrationTTL, topics.hydrationConcurr, log)
	projector := NewProjector(Deps{
		Store:                 valkeyStore,
		NC:                    nc,
		InvalidateSubject:     topics.invalidate,
		CacheInvalidatePrefix: topics.cacheInvalidate,
		Hydrator:              hydrator,
		Log:                   log,
	})

	registerConsumers(ctx, consumerRuntime{nrApp: nrApp, sub: sub, log: log}, projector, topics.stream)
	subscribeRPCs(rpcRuntime{
		nc: nc, store: valkeyStore, pub: pub, hydrator: hydrator, nrApp: nrApp, log: log,
	}, topics)
	fatalIf(log, bus.SubscribeRPCHealth(nc, serviceName, "projector-rpc"), "failed to subscribe rpc health")

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("projector ready",
		zap.String("status_subject", topics.status),
		zap.String("dashboard_subject", topics.dashboard),
		zap.String("stream_subject", topics.stream))

	<-ctx.Done()

	log.Info("projector shutting down")
}

// connectBus provisions the streams and opens the fleet's durable group
// subscriber, the RPC connection, and the JetStream publisher (the publisher
// escalates a cold live query onto the outgress system lane).
func connectBus(ctx context.Context, natsURL string, log *zap.Logger) (*nats.Conn, bus.Publisher, message.Subscriber) {
	fatalIf(log, bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log), "failed to provision jetstream streams")

	// One durable group for the whole projector fleet: each event is folded
	// into Valkey exactly once, and the durable consumer keeps its position
	// across restarts.
	sub, err := bus.NewSubscriber(natsURL, serviceName, log)
	fatalIf(log, err, "failed to connect subscriber")

	nc, err := bus.Connect(bus.RPCURL(natsURL), serviceName)
	fatalIf(log, err, "failed to connect nats")

	pub, err := bus.NewPublisher(natsURL, log)
	fatalIf(log, err, "failed to connect publisher")

	return nc, pub, sub
}

// registerConsumers binds the projector's fold handlers on the shared durable
// group. The stream-online event is a durable JetStream consumer (not a plain
// core Subscribe): it writes shared Valkey state and refreshes the shared
// projection, so exactly one projector pod must handle each event. Keyed by the
// projector's service group, pods share one consumer (one refresh per event,
// not pods x 3 hydration RPCs) and it survives restarts; other subsystems bind
// their own durable and still get every event once.
// consumerRuntime bundles the handles the fold consumers bind against.
type consumerRuntime struct {
	nrApp *newrelic.Application
	sub   message.Subscriber
	log   *zap.Logger
}

func registerConsumers(ctx context.Context, rt consumerRuntime, projector *Projector, streamTopic string) {
	bindings := []struct {
		subject string
		handle  func(*message.Message) error
	}{
		{data.SubjectUserChanged, projector.HandleUserChanged},
		{data.SubjectUserDeleted, projector.HandleUserDeleted},
		{data.SubjectModuleChanged, projector.HandleModuleChanged},
		{data.SubjectCommandChanged, projector.HandleCommandChanged},
		{streamTopic, projector.HandleStreamEvent},
	}
	for _, b := range bindings {
		fatalIf(rt.log, bus.Consume(ctx, rt.nrApp, rt.sub, b.subject, b.handle, rt.log),
			"failed to subscribe consumer: "+b.subject)
	}
}

// rpcRuntime bundles the runtime handles the projector's RPC surfaces bind
// against.
type rpcRuntime struct {
	nc       *nats.Conn
	store    *projection.Store
	pub      bus.Publisher
	hydrator *hydration.Hydrator
	nrApp    *newrelic.Application
	log      *zap.Logger
}

// subscribeRPCs binds the projector's request-reply surfaces: broadcaster
// status, the dashboard projection reads, and the live verb (which answers from
// the projection or escalates to Twitch via the outgress system lane).
func subscribeRPCs(rt rpcRuntime, topics projectorTopics) {
	fatalIf(rt.log, rpc.SubscribeStatus(rt.nc, rt.store, topics.status, topics.users, topics.invalidate, "projector-rpc", rt.nrApp, rt.log),
		"failed to subscribe status rpc")
	fatalIf(rt.log, rpc.SubscribeDashboard(rt.nc, rt.store, topics.dashboard,
		topics.commands, topics.modules, topics.cacheInvalidate, rt.hydrator, "projector-rpc", rt.nrApp, rt.log),
		"failed to subscribe dashboard projector rpc")
	fatalIf(rt.log, rpc.SubscribeLive(rt.nc, rt.store, rt.pub, topics.live, topics.outgressSystem, "projector-rpc", rt.nrApp, rt.log),
		"failed to subscribe live rpc")
}
