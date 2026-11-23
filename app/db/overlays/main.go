// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/db/overlays/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/overlays/ent/runtime"
	"ItsBagelBot/app/db/overlays/lapse"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/app/db/overlays/rpc"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"
)

const (
	serviceName = "overlays"
	queueGroup  = "overlays-rpc"
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core)
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return repository.MigrateSchema(ctx, client) })

	svcboot.FatalIf(log, bus.EnsureStreams(core.Ctx, core.NATSURL, []bus.StreamSpec{bus.ChannelEventsStream}, log), "failed to provision channel event stream")

	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()
	defer closeIntake()

	objects, err := objectstore.New(objectstore.ConfigFromEnv("R2_"))
	svcboot.FatalIf(log, err, "failed to configure object storage")

	repo := repository.New(client, n.Pub, objects, env.MustGet("R2_PUBLIC_URL"))
	svcboot.FatalIf(log, repo.MigratePowered(core.Ctx), "failed to migrate powered overlays")
	uploads := newAssets(core, repo, objects, n.RPC)
	serveUploads(core, uploads)
	sweeper := &lapse.Sweeper{Repo: repo, Plans: uploads.Plans}
	go sweeper.Run(core.Ctx, log)

	deleted := consumers.OnUserDeleted(serviceName, log, repo.DeleteAccount)
	svcboot.FatalIf(log, bus.Consume(core.Ctx, core.NR, n.Grouped, data.SubjectUserDeleted, deleted, log), "failed to subscribe to user deleted events")

	svcboot.FatalIf(log, bus.Consume(core.Ctx, core.NR, n.Grouped, data.SubjectUserBanned, bannedTts(repo, log), log), "failed to subscribe to user banned events")

	feedService, feedConn := newFeed(core, repo, n.RPC)
	defer feedConn.Close()
	svcboot.FatalIf(log, bus.Consume(core.Ctx, core.NR, n.Grouped, channelevents.EventsMatch, feedService.Consume, log), "failed to subscribe to channel events")

	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repo,
		Assets:    uploads,
	}
	svcboot.FatalIf(log, rpc.SubscribeDashboard(wiring), "failed to subscribe dashboard rpc")
	svcboot.FatalIf(log, rpc.SubscribeOwner(wiring), "failed to subscribe owner rpc")

	svcboot.FatalIf(log, rpc.SubscribeTts(wiring), "failed to subscribe tts rpc")
	svcboot.FatalIf(log, rpc.SubscribeOverlays(wiring), "failed to subscribe overlay rpc")
	svcboot.FatalIf(log, rpc.SubscribeAssets(wiring), "failed to subscribe asset rpc")
	svcboot.FatalIf(log, rpc.SubscribeFeed(wiring.RPCWiring, feedService), "failed to subscribe feed rpc")

	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	}, bus.LaneCheck("data", n.Grouped))

	log.Info("overlays service ready")

	core.Await()
}
