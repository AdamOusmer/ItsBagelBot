package main

import (
	"context"
	"os"
	"os/signal"
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

	"go.uber.org/zap"
)

const serviceName = "notifications"

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	// One-shot cron mode: `notifications cleanup` just fires the janitor verb at
	// the running service and exits, so the k3s CronJob reuses this same image.
	if len(os.Args) > 1 && os.Args[1] == "cleanup" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := runCleanup(ctx, log); err != nil {
			log.Fatal("notification cleanup failed", zap.Error(err))
		}
		return
	}

	nrApp, err := monitor.New(serviceName, log)
	if err != nil {
		log.Fatal("failed to start new relic", zap.Error(err))
	}
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
	if err != nil {
		log.Fatal("failed to open database", zap.Error(err))
	}

	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	if env.GetBool("DB_AUTO_MIGRATE", true) {
		if err := client.Schema.Create(ctx); err != nil {
			log.Fatal("failed to run migrations", zap.Error(err))
		}
	}

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	rpcURL := bus.RPCURL(natsURL)

	nc, err := bus.Connect(rpcURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	repo := repository.New(client)
	queueGroup := "notifications-rpc"
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
	if err := rpc.SubscribeAdmin(nc, repo, adminCfg, nrApp, log); err != nil {
		log.Fatal("failed to subscribe admin rpc", zap.Error(err))
	}

	userPrefix := env.Get("NATS_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.notifications")
	userCfg := rpc.UserConfig{
		Prefix:      userPrefix,
		QueueGroup:  queueGroup,
		FullReadTTL: fullReadTTL,
		PeekTTL:     peekTTL,
	}
	if err := rpc.SubscribeUser(nc, repo, userCfg, nrApp, log); err != nil {
		log.Fatal("failed to subscribe user rpc", zap.Error(err))
	}

	// Internal janitor verb driven by the k3s cron (see deploy/k8s). Not
	// exported from the NATS account, so only a client with the service's own
	// credentials can reach it.
	cleanupSubject := env.Get("NATS_NOTIFICATIONS_CLEANUP_SUBJECT", "bagel.rpc.internal.notifications.cleanup")
	if err := rpc.SubscribeMaintenance(nc, repo, cleanupSubject, queueGroup, nrApp, log); err != nil {
		log.Fatal("failed to subscribe maintenance rpc", zap.Error(err))
	}
	if err := bus.SubscribeRPCHealth(nc, serviceName, queueGroup); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("notifications service ready",
		zap.String("admin_prefix", adminPrefix),
		zap.String("user_prefix", userPrefix),
		zap.String("cleanup_subject", cleanupSubject))

	<-ctx.Done()

	log.Info("notifications service shutting down")
}
