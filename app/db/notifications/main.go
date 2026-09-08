// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/db/notifications/ent/runtime"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/app/db/notifications/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName = "notifications"
	queueGroup  = "notifications-rpc"
)

func main() {
	// One-shot cron mode: `notifications cleanup` just fires the janitor verb at
	// the running service and exits, so the k3s CronJob reuses this same image.
	// It stops short of svcboot.NewCore on purpose -- see svcboot.NewLogger.
	if len(os.Args) > 1 && os.Args[1] == "cleanup" {
		runCleanupMode()
		return
	}

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(log, "bagel_notifications")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	// Only the RPC connection, not the full MustNATS set: notifications
	// consumes no event lane, it only answers request/reply.
	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()

	repo := repository.New(client)
	invalidationPrefix := env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate")

	// TTL tiers (all Go durations). A send with no explicit expiry lives
	// defaultTTL globally so the cron eventually sweeps it; a full read hides it
	// from that user after fullReadTTL; opening the bell dropdown (peek) hides
	// an unread one after the longer, reduced peekTTL.
	defaultTTL := env.GetDuration("NOTIF_DEFAULT_TTL", 90*24*time.Hour)
	fullReadTTL := env.GetDuration("NOTIF_FULL_READ_TTL", 24*time.Hour)
	peekTTL := env.GetDuration("NOTIF_PEEK_TTL", 7*24*time.Hour)

	// Cross-service lookup so an admin can target a direct notification by
	// username, not just numeric id.
	userGetSubject := env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user") + ".get"

	adminPrefix := env.Get("NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.admin.notifications")
	adminCfg := rpc.AdminConfig{
		Prefix:             adminPrefix,
		InvalidationPrefix: invalidationPrefix,
		UserGetSubject:     userGetSubject,
		QueueGroup:         queueGroup,
		DefaultTTL:         defaultTTL,
	}
	svcboot.FatalIf(log, rpc.SubscribeAdmin(nc, repo, adminCfg, core.NR, log), "failed to subscribe admin rpc")

	userPrefix := env.Get("NATS_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.notifications")
	userCfg := rpc.UserConfig{
		Prefix:      userPrefix,
		QueueGroup:  queueGroup,
		FullReadTTL: fullReadTTL,
		PeekTTL:     peekTTL,
	}
	svcboot.FatalIf(log, rpc.SubscribeUser(nc, repo, userCfg, core.NR, log), "failed to subscribe user rpc")

	// Internal janitor verb driven by the k3s cron (see deploy/k8s). Not
	// exported from the NATS account, so only a client with the service's own
	// credentials can reach it.
	cleanupSubject := env.Get("NATS_NOTIFICATIONS_CLEANUP_SUBJECT", "bagel.rpc.internal.notifications.cleanup")
	svcboot.FatalIf(log, rpc.SubscribeMaintenance(nc, repo, cleanupSubject, queueGroup, core.NR, log),
		"failed to subscribe maintenance rpc")

	// No lane check: this service consumes no event lane, only request/reply.
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

// runCleanupMode fires the janitor verb at the running service and returns. It
// builds a logger but no APM app or signal context: the process lives for one
// RPC round trip.
func runCleanupMode() {
	log := svcboot.NewLogger(serviceName)
	defer func() { _ = log.Sync() }()

	ctx, cancel := context.WithTimeout(context.Background(), cleanupBudget)
	defer cancel()
	svcboot.FatalIf(log, runCleanup(ctx, log), "notification cleanup failed")
}

// cleanupBudget bounds the one-shot cron invocation end to end.
const cleanupBudget = 60 * time.Second
