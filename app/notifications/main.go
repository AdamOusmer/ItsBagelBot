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

	"ItsBagelBot/app/notifications/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/notifications/ent/runtime"
	"ItsBagelBot/app/notifications/repository"
	"ItsBagelBot/app/notifications/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"github.com/AdamOusmer/recipes/runtime"
	"github.com/AdamOusmer/recipes/svc/z2wz4"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const serviceName = "notifications"

// fatalIf aborts startup on err: notifications cannot run degraded without
// any of its core dependencies, so a failed step must crash the pod for
// Kubernetes to restart it.
func fatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	// One-shot cron mode: `notifications cleanup` just fires the janitor verb at
	// the running service and exits, so the k3s CronJob reuses this same image.
	if len(os.Args) > 1 && os.Args[1] == "cleanup" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		fatalIf(log, runCleanup(ctx, log), "notification cleanup failed")
		return
	}

	nrApp, err := monitor.New(serviceName, log)
	fatalIf(log, err, "failed to start new relic")
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", "bagel_notifications"),
	})
	fatalIf(log, err, "failed to open database")

	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	if env.GetBool("DB_AUTO_MIGRATE", true) {
		fatalIf(log, client.Schema.Create(ctx), "failed to run migrations")
	}

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	rpcURL := bus.RPCURL(natsURL)

	nc, busConn, k, grantsDenied := connectRPC(ctx, rpcURL, natsURL, log)
	defer nc.Close()
	defer busConn.Close()

	repo := repository.New(client)
	queueGroup := "notifications-rpc"
	// invalidationPrefix recovers "bagel.cache.invalidate" from notifications's
	// one cache-invalidate publish grant (ZCF3X, the exact leaf subject rather
	// than a parent wildcard).
	invalidationPrefix := strings.TrimSuffix(k.ZCF3X(), ".notifications")

	// TTL tiers (all Go durations). A send with no explicit expiry lives
	// defaultTTL globally so the cron eventually sweeps it; a full read hides it
	// from that user after fullReadTTL; opening the bell dropdown (peek) hides
	// an unread one after the longer, reduced peekTTL.
	defaultTTL := env.GetDuration("NOTIF_DEFAULT_TTL", 90*24*time.Hour)
	fullReadTTL := env.GetDuration("NOTIF_FULL_READ_TTL", 24*time.Hour)
	peekTTL := env.GetDuration("NOTIF_PEEK_TTL", 7*24*time.Hour)

	// Cross-service lookup so an admin can target a direct notification by
	// username, not just numeric id.
	userGetSubject := k.ZRTTK()

	adminPrefix := strings.TrimSuffix(k.ZIWGH(), ".>")
	adminCfg := rpc.AdminConfig{
		Prefix:             adminPrefix,
		InvalidationPrefix: invalidationPrefix,
		UserGetSubject:     userGetSubject,
		QueueGroup:         queueGroup,
		DefaultTTL:         defaultTTL,
	}
	fatalIf(log, rpc.SubscribeAdmin(nc, repo, adminCfg, nrApp, log), "failed to subscribe admin rpc")

	userPrefix := strings.TrimSuffix(k.ZDVSN(), ".>")
	userCfg := rpc.UserConfig{
		Prefix:      userPrefix,
		QueueGroup:  queueGroup,
		FullReadTTL: fullReadTTL,
		PeekTTL:     peekTTL,
	}
	fatalIf(log, rpc.SubscribeUser(nc, repo, userCfg, nrApp, log), "failed to subscribe user rpc")

	// Internal janitor verb driven by the k3s cron (see deploy/k8s). Not
	// exported from the NATS account, so only a client with the service's own
	// credentials can reach it.
	cleanupSubject := env.Get("NATS_NOTIFICATIONS_CLEANUP_SUBJECT", "bagel.rpc.internal.notifications.cleanup")
	fatalIf(log, rpc.SubscribeMaintenance(nc, repo, cleanupSubject, queueGroup, nrApp, log), "failed to subscribe maintenance rpc")
	health.Serve(env.Get("LISTEN_ADDR", ":8080"), serviceName,
		health.Bool("nats", nc.IsConnected),
		health.Bool("nats_grants", func() bool { return !grantsDenied.Load() }),
	)

	log.Info("notifications service ready",
		zap.String("admin_prefix", adminPrefix),
		zap.String("user_prefix", userPrefix),
		zap.String("cleanup_subject", cleanupSubject))

	<-ctx.Done()

	log.Info("notifications service shutting down")
}

// connectRPC dials the RPC connection, binds notifications's recipes binding
// over it and a separate BUS-plane connection, wires its permission-violation
// Watchdog, and preflights the (empty — notifications touches no JetStream
// stream) grant set it declares.
func connectRPC(ctx context.Context, url, natsURL string, log *zap.Logger) (*nats.Conn, *nats.Conn, *z2wz4.K, *atomic.Bool) {
	nc, err := bus.Connect(url, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	if err := bus.SubscribeRPCHealth(nc, serviceName, "notifications-rpc"); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}

	busConn, err := bus.ConnectBus(natsURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect bus-plane nats", zap.Error(err))
	}

	k, err := z2wz4.Up(z2wz4.U{Bus: busConn, Rpc: nc})
	if err != nil {
		log.Fatal("failed to bind notifications's recipes binding", zap.Error(err))
	}

	// grantsDenied flips true the first time the NATS server reports
	// notifications's BUS account denied a permission its manifest declares
	// (see runtime.Watchdog); the returned pointer backs the nats_grants
	// health check.
	grantsDenied := &atomic.Bool{}
	watchdog := runtime.NewWatchdog(k.M(), func(subject, canonical string, err error) {
		log.Error("nats permission violation",
			zap.String("subject", subject), zap.String("canonical", canonical), zap.Error(err))
	}, func() { grantsDenied.Store(true) })
	bus.GuardConnection(busConn, watchdog.Handler())

	if err := runtime.PreflightStreams(ctx, busConn, k.Expectations(), 0); err != nil {
		log.Fatal("recipes preflight failed: missing jetstream grant(s)", zap.Error(err))
	}

	return nc, busConn, k, grantsDenied
}
