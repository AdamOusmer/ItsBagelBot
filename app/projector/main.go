// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
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
	"ItsBagelBot/pkg/svcboot"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	serviceName = "projector"
	queueGroup  = "projector-rpc"
)

// dataTierServices are the services the projector's own /status answers for.
// The public health.itsbagelbot.com/db endpoint terminates here, so each one is
// folded in over its own health RPC rather than re-checked from this pod: the
// downstream's checks arrive by name, and a users pod degraded by its own MySQL
// surfaces as users(mysql: ...) instead of disappearing behind a bool.
//
// transactions is deliberately absent. Billing answers on its own endpoint
// (health.itsbagelbot.com/billing) and checks itself, because a payment
// processor outage is not a data-tier outage and must not page as one.
var dataTierServices = []string{"users", "commands", "modules", "loyalty", "notifications", "discord-data"}

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
	streamInfo        string
	loyalty           string
	outgressSystem    string
	hydrationConcurr  int
	queryHydrationTTL time.Duration
	liveHydrationTTL  time.Duration
}

func loadTopics() projectorTopics {
	return projectorTopics{
		stream:          env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
		users:           env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		modules:         env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get"),
		commands:        env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get"),
		invalidate:      env.Get("NATS_PROJECTOR_TIER_INVALIDATE_SUBJECT", "bagel.internal.projector.tier.invalidate"),
		cacheInvalidate: env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),
		status:          env.Get("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
		dashboard:       env.Get("NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.projector.dashboard"),
		live:            env.Get("NATS_BROADCASTER_LIVE_SUBJECT", "bagel.rpc.broadcaster.live.get"),
		streamInfo:      env.Get("NATS_BROADCASTER_STREAM_INFO_SUBJECT", "bagel.rpc.broadcaster.stream_info.get"),
		// loyalty is the RPC prefix the loyalty service subscribes under (see
		// app/db/loyalty/rpc/rpc.go's Subscribe doc). Same default the dashboard's
		// services.ts uses for its own loyalty client, so both sides of the
		// Overview counter feature point at the same service without extra
		// config in dev.
		loyalty:           env.Get("NATS_LOYALTY_SUBJECT_PREFIX", "bagel.rpc.loyalty"),
		outgressSystem:    env.Get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"),
		hydrationConcurr:  env.GetInt("PROJECTOR_HYDRATION_CONCURRENCY", 8),
		queryHydrationTTL: env.GetDuration("PROJECTOR_QUERY_HYDRATION_TTL", 2*time.Hour),
		liveHydrationTTL:  env.GetDuration("PROJECTOR_LIVE_HYDRATION_TTL", projection.DefaultTTL),
	}
}

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx, nrApp := core.Log, core.Ctx, core.NR

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()
	valkeyStore := projection.NewStore(valkeyClient)

	nc, pub, sub := connectBus(core)
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
		Loyalty:               newLoyaltyCounters(nc, topics.loyalty),
		Log:                   log,
	})

	registerConsumers(ctx, consumerRuntime{nrApp: nrApp, sub: sub, log: log}, projector, topics.stream)
	subscribeRPCs(rpcRuntime{
		nc: nc, store: valkeyStore, pub: pub, hydrator: hydrator, nrApp: nrApp, log: log,
	}, topics)
	svcboot.ServeHealth(svcboot.Health{
		Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
	}, tierChecks(nc, sub)...)

	log.Info("projector ready",
		zap.String("status_subject", topics.status),
		zap.String("dashboard_subject", topics.dashboard),
		zap.String("stream_subject", topics.stream))

	core.Await()
}

// tierChecks builds the projector's health surface: its own dependencies plus
// one probe per data-tier service, so /status here is the whole tier's answer
// on one endpoint.
//
// The probes are deliberately not wrapped in health.Degrades. HealthProbe
// already carries the downstream's own verdict -- down fails this check,
// degraded degrades it -- and marking them optional would flatten a users
// outage into an impairment nobody gets paged for.
//
// Each aggregated service keeps its own MySQL check instead of the projector
// holding one for the tier: the schemas are expected to split across servers,
// and a single hoisted check could not say which database went.
func tierChecks(nc *nats.Conn, sub bus.Subscriber) []health.Check {
	checks := []health.Check{
		// One durable group carries every fold: the twitch.ingress.event.stream
		// lane and the data.> subjects share this subscriber, so the check is
		// per-group rather than per-subject. It catches a consumer that stays
		// bound while failing to fetch -- projections stop refreshing, the
		// connection stays up, and every other check reads green.
		bus.LaneCheck("stream", sub),
	}
	for _, service := range dataTierServices {
		checks = append(checks, bus.HealthProbe(nc, service))
	}
	return checks
}

// connectBus reconciles the streams the projector reads, then opens the
// fleet's durable group subscriber, the RPC connection, and the JetStream
// publisher (the publisher escalates a cold live query onto the outgress
// system lane).
//
// The projector provisions its own inputs rather than depending on the users
// or sesame pods having booted first: every owner reconciles the same catalog
// spec, so whoever arrives first creates and everyone else converges on an
// identical no-op update. The ingress lanes go through IngressLaneSpecs so
// the partition flag ordering (narrow before create) is preserved here exactly
// as it is in sesame. The trade is deliberate: the projector credential can
// now mutate streams it reads, bounded by per-stream ACL grants.
func connectBus(core svcboot.Core) (*nats.Conn, bus.Publisher, bus.Subscriber) {
	log := core.Log
	specs := append([]bus.StreamSpec{bus.BagelDataStream}, bus.IngressLaneSpecs()...)
	svcboot.FatalIf(log, bus.EnsureStreams(core.Ctx, core.NATSURL, specs, log), "failed to provision projector streams")

	// One durable group for the whole projector fleet: each event is folded
	// into Valkey exactly once, and the durable consumer keeps its position
	// across restarts.
	sub, err := bus.NewSubscriber(core.NATSURL, serviceName, log)
	svcboot.FatalIf(log, err, "failed to connect subscriber")

	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))

	pub, err := bus.NewPublisher(core.NATSURL, log)
	svcboot.FatalIf(log, err, "failed to connect publisher")

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
	sub   bus.Subscriber
	log   *zap.Logger
}

func registerConsumers(ctx context.Context, rt consumerRuntime, projector *Projector, streamTopic string) {
	bindings := []struct {
		subject string
		handle  func(*bus.Message) error
	}{
		{data.SubjectUserChanged, projector.HandleUserChanged},
		{data.SubjectUserDeleted, projector.HandleUserDeleted},
		{data.SubjectModuleChanged, projector.HandleModuleChanged},
		{data.SubjectCommandChanged, projector.HandleCommandChanged},
		{streamTopic, projector.HandleStreamEvent},
	}
	for _, b := range bindings {
		svcboot.FatalIf(rt.log, bus.Consume(ctx, rt.nrApp, rt.sub, b.subject, b.handle, rt.log),
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
	svcboot.FatalIf(rt.log, rpc.SubscribeStatus(rt.nc, rt.store, topics.status, topics.users, topics.invalidate, queueGroup, rt.nrApp, rt.log),
		"failed to subscribe status rpc")
	svcboot.FatalIf(rt.log, rpc.SubscribeDashboard(rt.nc, rt.store, topics.dashboard,
		topics.commands, topics.modules, topics.cacheInvalidate, rt.hydrator, queueGroup, rt.nrApp, rt.log),
		"failed to subscribe dashboard projector rpc")
	svcboot.FatalIf(rt.log, rpc.SubscribeLive(rt.nc, rt.store, rt.pub, topics.live, topics.outgressSystem, queueGroup, rt.nrApp, rt.log),
		"failed to subscribe live rpc")
	svcboot.FatalIf(rt.log, rpc.SubscribeStreamInfo(rpc.StreamInfoDeps{
		NC: rt.nc, Store: rt.store, Subject: topics.streamInfo,
		QueueGroup: queueGroup, App: rt.nrApp, Log: rt.log,
	}), "failed to subscribe stream info rpc")
}
