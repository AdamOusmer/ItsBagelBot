// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/discord/ent/runtime"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/app/db/discord/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName    = "discord-data"
	queueGroup     = "discord-data-rpc"
	defaultSchema  = "bagel_discord"
	defaultNATSURL = "nats://127.0.0.1:4222"
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, defaultSchema)
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	nc := svcboot.MustRPCConn(core, rpcEndpoint())
	defer nc.Close()

	prefix := env.Get("NATS_DISCORD_DATA_SUBJECT_PREFIX", "bagel.rpc.discord-data")
	err := rpc.Subscribe(rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repository.New(client, driver.Dialect()),
		Prefix:    prefix,
	})
	if err != nil {
		log.Fatal("failed to subscribe discord-data rpc", zap.Error(err))
	}

	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	})

	log.Info("discord-data service ready", zap.String("prefix", prefix))

	core.Await()
}

// Must not become svcboot.MustNATS: discord-data has no BUS credentials.
func rpcEndpoint() string {
	return bus.RPCURL(env.Get("NATS_URL", defaultNATSURL))
}
