// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/db/overlays/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/overlays/ent/runtime"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/app/db/overlays/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
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

	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()
	defer closeIntake()

	repo := repository.New(client, n.Pub)

	deleted := consumers.OnUserDeleted(serviceName, log, repo.DeleteAccount)
	svcboot.FatalIf(log, bus.Consume(core.Ctx, core.NR, n.Grouped, data.SubjectUserDeleted, deleted, log), "failed to subscribe to user deleted events")

	svcboot.FatalIf(log, bus.Consume(core.Ctx, core.NR, n.Grouped, data.SubjectUserBanned, bannedTts(repo, log), log), "failed to subscribe to user banned events")

	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repo,
	}
	svcboot.FatalIf(log, rpc.SubscribeDashboard(wiring), "failed to subscribe dashboard rpc")
	svcboot.FatalIf(log, rpc.SubscribeOwner(wiring), "failed to subscribe owner rpc")

	svcboot.FatalIf(log, rpc.SubscribeTts(wiring), "failed to subscribe tts rpc")

	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	}, bus.LaneCheck("data", n.Grouped))

	log.Info("overlays service ready")

	core.Await()
}
