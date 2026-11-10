// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/notifications/ent/runtime"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/app/db/notifications/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName = "notifications"
	queueGroup  = "notifications-rpc"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "cleanup" {
		runCleanupMode()
		return
	}

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, "bagel_notifications")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return repository.MigrateSchema(ctx, client) })

	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()

	repo := repository.New(client)
	invalidationPrefix := conf.CacheInvalidationPrefix.Get()

	defaultTTL := env.GetDuration("NOTIF_DEFAULT_TTL", 90*24*time.Hour)
	fullReadTTL := env.GetDuration("NOTIF_FULL_READ_TTL", 24*time.Hour)
	peekTTL := env.GetDuration("NOTIF_PEEK_TTL", 7*24*time.Hour)

	userGetSubject := conf.UsersGetSubject.Get()

	adminPrefix := conf.AdminNotifications.Get()
	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repo,
	}
	adminCfg := rpc.AdminConfig{
		Prefix:             adminPrefix,
		InvalidationPrefix: invalidationPrefix,
		UserGetSubject:     userGetSubject,
		DefaultTTL:         defaultTTL,
	}
	svcboot.FatalIf(log, rpc.SubscribeConnection(wiring, invalidationPrefix), "failed to subscribe connection rpc")
	svcboot.FatalIf(log, rpc.SubscribeAdmin(wiring, adminCfg), "failed to subscribe admin rpc")

	userPrefix := env.Get("NATS_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.notifications")
	userCfg := rpc.UserConfig{
		UserGetSubject:     userGetSubject,
		Prefix:             userPrefix,
		InvalidationPrefix: invalidationPrefix,
		FullReadTTL:        fullReadTTL,
		PeekTTL:            peekTTL,
	}
	svcboot.FatalIf(log, rpc.SubscribeUser(wiring, userCfg), "failed to subscribe user rpc")

	// Must stay unexported from the NATS account.
	cleanupSubject := conf.NotificationsCleanup.Get()
	svcboot.FatalIf(log, rpc.SubscribeMaintenance(wiring, cleanupSubject), "failed to subscribe maintenance rpc")

	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	})

	log.Info("notifications service ready",
		zap.String("admin_prefix", adminPrefix),
		zap.String("user_prefix", userPrefix),
		zap.String("cleanup_subject", cleanupSubject))

	core.Await()
}

func runCleanupMode() {
	log := svcboot.NewLogger(serviceName)
	defer func() { _ = log.Sync() }()

	ctx, cancel := context.WithTimeout(context.Background(), cleanupBudget)
	defer cancel()
	svcboot.FatalIf(log, runCleanup(ctx, log), "notification cleanup failed")
}

const cleanupBudget = 60 * time.Second
