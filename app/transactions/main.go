package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/transactions/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/transactions/ent/runtime"
	"ItsBagelBot/app/transactions/repository"
	"ItsBagelBot/app/transactions/rpc"
	"ItsBagelBot/app/transactions/tebex"
	"ItsBagelBot/app/transactions/web"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

const serviceName = "transactions"

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
		Schema:   env.Get("DB_SCHEMA", "bagel_transactions"),
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

	if err := bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	repo := repository.NewTransactions(client, pub)

	// RPC-plane connection (TRANSACTIONS_RPC account): answers the checkout
	// basket verb and issues the recipient-lookup / gift-notification requests.
	nc, err := bus.Connect(bus.RPCURL(natsURL), serviceName)
	if err != nil {
		log.Fatal("failed to connect rpc nats", zap.Error(err))
	}
	defer nc.Close()

	// Checkout RPC (dashboard -> basket_create). Optional: without the Tebex
	// Headless credentials the service stays webhook-only, exactly as before.
	checkoutConfigured := false
	// TEBEX_HEADLESS_TOKEN is the legacy name for the same webstore public token.
	webstoreToken := env.Get("TEBEX_WEBSTORE_TOKEN", env.Get("TEBEX_HEADLESS_TOKEN", ""))
	privateKey := env.Get("TEBEX_PRIVATE_KEY", env.Get("TEBEX_SECRET_KEY", env.Get("TEBEX_API_PRIVATE_KEY", "")))
	packageID := env.GetInt("TEBEX_PACKAGE_ID", 0)
	if webstoreToken != "" && packageID > 0 {
		dashboardOrigin := env.Get("DASHBOARD_ORIGIN", "https://dashboard.itsbagelbot.com")
		tebexClient, err := tebex.New(tebex.Config{
			WebstoreToken: webstoreToken,
			PrivateKey:    privateKey,
			PackageID:     packageID,
			PackageType:   env.Get("TEBEX_PACKAGE_TYPE", "subscription"),
			CompleteURL:   dashboardOrigin + "/billing?checkout=complete",
			CancelURL:     dashboardOrigin + "/billing?checkout=cancelled",
		})
		if err != nil {
			log.Fatal("failed to build tebex client", zap.Error(err))
		}

		userGetSubject := env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user") + ".get"
		prefix := env.Get("NATS_TRANSACTIONS_SUBJECT_PREFIX", "bagel.rpc.transactions")
		if err := rpc.SubscribeCheckout(nc, tebexClient, prefix, userGetSubject, "transactions-rpc", nrApp, log); err != nil {
			log.Fatal("failed to subscribe checkout rpc", zap.Error(err))
		}
		checkoutConfigured = true
	} else {
		log.Warn("tebex checkout rpc disabled: TEBEX_WEBSTORE_TOKEN / TEBEX_PACKAGE_ID not configured")
	}

	sendSubject := env.Get("NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.admin.notifications") + ".send"
	notifier := rpc.NewGiftNotifier(nc, sendSubject)
	billingSubject := env.Get("NATS_INTERNAL_BILLING_SUBJECT", "bagel.rpc.internal.billing.apply")
	billing := rpc.NewBillingApplier(nc, billingSubject)

	listenAddr := env.Get("LISTEN_ADDR", ":8080")
	httpApp := web.New(repo, web.Config{
		WebhookSecret: env.Get("TEBEX_WEBHOOK_SECRET", ""),
		NotifyGift:    notifier.Notify,
		ApplyBilling:  billing.Apply,
	}, log.Named("http"))

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpApp.Listen(listenAddr)
	}()

	log.Info("transactions service ready",
		zap.String("listen_addr", listenAddr),
		zap.Bool("tebex_webhook_configured", env.Get("TEBEX_WEBHOOK_SECRET", "") != ""),
		zap.Bool("tebex_checkout_configured", checkoutConfigured),
		zap.Bool("tebex_checkout_auth_configured", privateKey != ""),
	)

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		log.Fatal("transactions http server stopped", zap.Error(err))
	}

	log.Info("transactions service shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpApp.ShutdownWithContext(shutdownCtx); err != nil {
		log.Warn("transactions http server shutdown failed", zap.Error(err))
	}
}
