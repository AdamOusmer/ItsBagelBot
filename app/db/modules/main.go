// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/db/modules/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/db/modules/ent/runtime"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/db/modules/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	serviceName = "modules"
	queueGroup  = "modules-rpc"
)

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, "bagel_modules")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()

	repo := repository.NewModules(client, n.Pub, core.NR, log)
	defer repo.Close(context.Background()) // flushes pending writes on shutdown
	defer closeIntake()                    // stops intake before the repo flush above

	quotes := repository.NewQuotes(client, log)

	consumeEvents(core.Ctx, eventsWiring{
		app: core.NR, broadcast: n.Broadcast, grouped: n.Grouped,
		repo: repo, quotes: quotes, log: log,
	})

	projectionSubject := subscribeRPCs(rpcWiring{
		nc: n.RPC, client: client, repo: repo, quotes: quotes, app: core.NR, log: log,
	})
	// The lane check covers the durable group folding data.reproject.request
	// and data.users.deleted: a consumer that stays bound while failing to
	// fetch leaves reprojection requests unanswered, with NATS and MySQL both
	// still reading green. The broadcast subscriber is not checked -- it has no
	// fetch loop to wedge.
	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	}, bus.LaneCheck("data", n.Grouped))

	log.Info("modules service ready", zap.String("projection_subject", projectionSubject))

	core.Await()
}

// eventsWiring bundles what consumeEvents needs so the wiring reads as one
// value instead of a long parameter list.
type eventsWiring struct {
	app       *newrelic.Application
	broadcast bus.Subscriber
	grouped   bus.Subscriber
	repo      *repository.Modules
	quotes    *repository.Quotes
	log       *zap.Logger
}

// consumeEvents attaches the service's event subscriptions: cache invalidation
// on the broadcast subscriber, reprojection and account deletion on the
// durable group. Fatal on any subscribe failure, matching main's boot style.
func consumeEvents(ctx context.Context, w eventsWiring) {
	invalidate := consumers.OnChangeInvalidate(changedUserID, w.repo.Invalidate)
	if err := bus.Consume(ctx, w.app, w.broadcast, data.SubjectModuleChanged, invalidate, w.log); err != nil {
		w.log.Fatal("failed to subscribe to module changes", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectReprojectRequest, func(*bus.Message) error {
		return w.repo.Reproject(ctx)
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to reproject requests", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectUserDeleted, deleteUser(w), w.log); err != nil {
		w.log.Fatal("failed to subscribe to user deleted events", zap.Error(err))
	}
}

// changedUserID reads the account off a module change event. Go cannot reach
// a field through a type parameter, so the shared invalidation consumer takes
// this accessor rather than a reflective one.
func changedUserID(dto data.ModuleChangedDTO) uint64 { return dto.UserID }

// deleteUser sweeps a deleted account's module rows and quote book. The
// payload guards and the log line are shared with the other data services;
// only the two sweeps are this service's own.
func deleteUser(w eventsWiring) func(*bus.Message) error {
	return consumers.OnUserDeleted(serviceName, w.log, func(ctx context.Context, userID uint64) error {
		if err := w.repo.DeleteAllForUser(ctx, userID); err != nil {
			return err
		}
		return w.quotes.DeleteAllForUser(ctx, userID)
	})
}

// rpcWiring bundles what subscribeRPCs needs, mirroring eventsWiring.
type rpcWiring struct {
	nc     *nats.Conn
	client *ent.Client
	repo   *repository.Modules
	quotes *repository.Quotes
	app    *newrelic.Application
	log    *zap.Logger
}

// subscribeRPCs answers the service's request/reply verbs: the internal
// projection read, the dashboard verbs, the channel-quotes verbs and the
// personality verbs. Returns the projection subject for the ready banner.
// Fatal on any subscribe failure, matching main's boot style.
func subscribeRPCs(w rpcWiring) string {
	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: w.nc, App: w.app, Queue: queueGroup, Log: w.log},
		Repo:      w.repo,
	}

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get")
	if err := rpc.SubscribeProjection(wiring, projectionSubject); err != nil {
		w.log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	// Dashboard verbs (list, upsert): the console toggles/configures modules the
	// same way it manages commands.
	dashboardSubject := env.Get("NATS_MODULES_SUBJECT_PREFIX", "bagel.rpc.modules")
	if err := rpc.SubscribeDashboard(wiring, dashboardSubject); err != nil {
		w.log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}

	// Channel-quotes verbs (the sesame quotes module's store).
	if err := rpc.SubscribeQuotes(wiring.RPCWiring, w.quotes, dashboardSubject+".quote"); err != nil {
		w.log.Fatal("failed to subscribe quotes rpc", zap.Error(err))
	}

	// Personality verbs (the sesame personality module's permanent feed counter).
	personality := repository.NewPersonality(w.client)
	if err := rpc.SubscribePersonality(wiring.RPCWiring, personality, dashboardSubject+".personality"); err != nil {
		w.log.Fatal("failed to subscribe personality rpc", zap.Error(err))
	}
	return projectionSubject
}
