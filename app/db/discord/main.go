// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
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
	serviceName = "discord-data"
	queueGroup  = "discord-data-rpc"
	// defaultSchema is the per-service MySQL schema (ADR 0007: one schema per
	// data service, no cross-schema foreign keys).
	defaultSchema = "bagel_discord"
	// defaultNATSURL is the local-development endpoint. Production overrides
	// it (and NATS_RPC_URL) from the manifest.
	defaultNATSURL = "nats://127.0.0.1:4222"
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	// Boot order is load-bearing: driver -> client -> migrate -> NATS -> repo
	// -> rpc -> health. The health Set reports on the pool, so it cannot be
	// built before the pool exists, and the RPC surface must not answer before
	// the schema it queries is there.
	driver := databoot.MustEntDriver(log, defaultSchema)
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	nc := svcboot.MustRPCConn(core, rpcEndpoint())
	defer nc.Close()

	prefix := env.Get("NATS_DISCORD_DATA_SUBJECT_PREFIX", "bagel.rpc.discord-data")
	err := rpc.Subscribe(rpc.Wiring{
		NC:         nc,
		Repo:       repository.New(client, driver.Dialect()),
		Prefix:     prefix,
		QueueGroup: queueGroup,
		App:        core.NR,
		Log:        log,
	})
	if err != nil {
		log.Fatal("failed to subscribe discord-data rpc", zap.Error(err))
	}

	// No lane check: this service consumes no event lane, only request/reply.
	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	})

	log.Info("discord-data service ready", zap.String("prefix", prefix))

	core.Await()
}

// rpcEndpoint resolves the one NATS endpoint this service dials.
//
// discord-data is RPC-only, like notifications: it answers request/reply and
// consumes no JetStream lane, so it holds no BUS identity at all. That is why
// this does not go through svcboot.MustNATS, which additionally opens a
// JetStream publisher and two subscribers on the shared BUS account -- with
// NO_BUS set for discord_data in deploy/messaging/nats-secrets.py those
// credentials are never minted, and the manifest injects only NATS_RPC_USER /
// NATS_RPC_PASSWORD, so MustNATS would fail authorization at boot rather than
// merely opening connections nothing uses.
func rpcEndpoint() string {
	return bus.RPCURL(env.Get("NATS_URL", defaultNATSURL))
}
