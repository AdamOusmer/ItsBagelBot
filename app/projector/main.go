// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"ItsBagelBot/app/projector/hydration"
	"ItsBagelBot/app/projector/rpc"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/AdamOusmer/recipes/runtime"
	"github.com/AdamOusmer/recipes/svc/z2ezc"
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

// loadTopics resolves the projection RPC / hydration subjects from k, the
// recipes binding, except invalidate: NATS_PROJECTOR_TIER_INVALIDATE_SUBJECT
// has no grant in recipes' manifest yet, so it stays env-sourced.
func loadTopics(k *z2ezc.K) projectorTopics {
	return projectorTopics{
		stream:     k.ZUPT4().Subject,
		users:      k.ZRML7(),
		modules:    k.ZIE27(),
		commands:   k.ZGRZH(),
		invalidate: env.Get("NATS_PROJECTOR_TIER_INVALIDATE_SUBJECT", "bagel.internal.projector.tier.invalidate"),
		// cacheInvalidate recovers "bagel.cache.invalidate" from one of
		// projector's three separate cache-invalidate publish grants
		// (commands/live/modules): the binding grants each concrete subject
		// rather than a shared parent wildcard.
		cacheInvalidate: strings.TrimSuffix(k.ZGOU3(), ".commands"),
		status:          k.ZIFLM(),
		dashboard:       strings.TrimSuffix(k.ZZ2MX(), ".>"),
		live:            k.ZP3WY(),
		outgressSystem:  k.ZTSKM(),

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
	nc, k, grantsDenied, pub, sub := connectBus(ctx, natsURL, log)
	defer nc.Close()
	defer func() { _ = pub.Close() }()
	defer func() { _ = sub.Close() }()

	topics := loadTopics(k)
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

	registerConsumers(ctx, consumerRuntime{nrApp: nrApp, sub: sub, log: log}, projector, k, topics.stream)
	subscribeRPCs(rpcRuntime{
		nc: nc, store: valkeyStore, pub: pub, hydrator: hydrator, nrApp: nrApp, log: log,
	}, topics)
	fatalIf(log, bus.SubscribeRPCHealth(nc, serviceName, "projector-rpc"), "failed to subscribe rpc health")

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), serviceName,
		health.Bool("nats", nc.IsConnected),
		health.Bool("nats_grants", func() bool { return !grantsDenied.Load() }),
	)

	log.Info("projector ready",
		zap.String("status_subject", topics.status),
		zap.String("dashboard_subject", topics.dashboard),
		zap.String("stream_subject", topics.stream))

	<-ctx.Done()

	log.Info("projector shutting down")
}

// connectBus dials the RPC and BUS-plane connections, binds projector's
// recipes binding, wires its permission-violation Watchdog, preflights the
// JetStream grants it declares, then reconciles the streams the projector
// reads: k.ZNT6V() (recipes' manifest, see recipes/MAP.md "projector
// manage") is now the source of truth for that shape, replacing the old
// BagelDataStream+IngressLaneSpecs construction. It then opens the fleet's
// durable group subscriber and the JetStream publisher (which escalates a
// cold live query onto the outgress system lane).
//
// The projector provisions its own inputs rather than depending on the users
// or sesame pods having booted first: every owner reconciles the same catalog
// spec, so whoever arrives first creates and everyone else converges on an
// identical no-op update. The trade is deliberate: the projector credential
// can now mutate streams it reads, bounded by per-stream ACL grants.
func connectBus(ctx context.Context, natsURL string, log *zap.Logger) (*nats.Conn, *z2ezc.K, *atomic.Bool, bus.Publisher, bus.Subscriber) {
	nc, err := bus.Connect(bus.RPCURL(natsURL), serviceName)
	fatalIf(log, err, "failed to connect nats")

	busConn, err := bus.ConnectBus(natsURL, serviceName)
	fatalIf(log, err, "failed to connect bus-plane nats")

	k, err := z2ezc.Up(z2ezc.U{Bus: busConn, Rpc: nc})
	fatalIf(log, err, "failed to bind projector's recipes binding")

	// grantsDenied flips true the first time the NATS server reports
	// projector's BUS account denied a permission its manifest declares (see
	// runtime.Watchdog); the returned pointer backs the nats_grants health
	// check.
	grantsDenied := &atomic.Bool{}
	watchdog := runtime.NewWatchdog(k.M(), func(subject, canonical string, err error) {
		log.Error("nats permission violation",
			zap.String("subject", subject), zap.String("canonical", canonical), zap.Error(err))
	}, func() { grantsDenied.Store(true) })
	bus.GuardConnection(busConn, watchdog.Handler())

	fatalIf(log, runtime.PreflightStreams(ctx, busConn, k.Expectations(), 0),
		"recipes preflight failed: missing jetstream grant(s)")

	fatalIf(log, bus.EnsureStreams(ctx, natsURL, k.ZNT6V(), log), "failed to provision projector streams")

	// One durable group for the whole projector fleet: each event is folded
	// into Valkey exactly once, and the durable consumer keeps its position
	// across restarts.
	sub, err := bus.NewSubscriber(natsURL, serviceName, log)
	fatalIf(log, err, "failed to connect subscriber")

	pub, err := bus.NewPublisher(natsURL, log)
	fatalIf(log, err, "failed to connect publisher")

	return nc, k, grantsDenied, pub, sub
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
	sub   bus.Subscriber
	log   *zap.Logger
}

func registerConsumers(ctx context.Context, rt consumerRuntime, projector *Projector, k *z2ezc.K, streamTopic string) {
	bindings := []struct {
		subject string
		handle  func(*bus.Message) error
	}{
		{k.ZSD7J().Subject, projector.HandleUserChanged},
		{k.ZN747().Subject, projector.HandleUserDeleted},
		{k.ZBKXM().Subject, projector.HandleModuleChanged},
		{k.ZRKWQ().Subject, projector.HandleCommandChanged},
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
