package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

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

	// Cross-service lookup so an admin can target a direct notification by
	// username, not just numeric id.
	userGetSubject := env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user") + ".get"

	adminPrefix := env.Get("NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.admin.notifications")
	adminCfg := rpc.AdminConfig{
		Prefix:             adminPrefix,
		InvalidationPrefix: invalidationPrefix,
		UserGetSubject:     userGetSubject,
		QueueGroup:         queueGroup,
	}
	if err := rpc.SubscribeAdmin(nc, repo, adminCfg, nrApp, log); err != nil {
		log.Fatal("failed to subscribe admin rpc", zap.Error(err))
	}

	userPrefix := env.Get("NATS_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.notifications")
	if err := rpc.SubscribeUser(nc, repo, rpc.UserConfig{Prefix: userPrefix, QueueGroup: queueGroup}, nrApp, log); err != nil {
		log.Fatal("failed to subscribe user rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("notifications service ready",
		zap.String("admin_prefix", adminPrefix),
		zap.String("user_prefix", userPrefix))

	<-ctx.Done()

	log.Info("notifications service shutting down")
}
