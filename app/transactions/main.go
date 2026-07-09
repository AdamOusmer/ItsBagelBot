package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/transactions/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/transactions/ent/runtime"
	"ItsBagelBot/app/transactions/mail"
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

	repo := repository.NewTransactions(client)

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
	dashboardOrigin := env.Get("DASHBOARD_ORIGIN", "https://dashboard.itsbagelbot.com")
	if webstoreToken != "" && packageID > 0 {
		tebexClient, err := tebex.New(tebex.Config{
			WebstoreToken:   webstoreToken,
			PrivateKey:      privateKey,
			IncludeUsername: env.GetBool("TEBEX_INCLUDE_USERNAME", false),
			PackageID:       packageID,
			PackageType:     env.Get("TEBEX_PACKAGE_TYPE", "subscription"),
			CompleteURL:     dashboardOrigin + "/billing?checkout=complete",
			CancelURL:       dashboardOrigin + "/billing?checkout=cancelled",
		})
		if err != nil {
			log.Fatal("failed to build tebex client", zap.Error(err))
		}

		userGetSubject := env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user") + ".get"
		prefix := env.Get("NATS_TRANSACTIONS_SUBJECT_PREFIX", "bagel.rpc.transactions")
		if err := rpc.SubscribeCheckout(
			rpc.CheckoutRuntime{NC: nc, App: nrApp, Log: log},
			tebexClient,
			rpc.CheckoutConfig{Prefix: prefix, UserGetSubject: userGetSubject, QueueGroup: "transactions-rpc"},
		); err != nil {
			log.Fatal("failed to subscribe checkout rpc", zap.Error(err))
		}
		checkoutConfigured = true
	} else {
		log.Warn("tebex checkout rpc disabled: TEBEX_WEBSTORE_TOKEN / TEBEX_PACKAGE_ID not configured")
	}

	sendSubject := env.Get("NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.admin.notifications") + ".send"

	// Gift email channel (Resend). Optional: without the API key the notifier
	// keeps sending the in-app notification only, exactly as before.
	var mailer *mail.Mailer
	// RESEND_API is the Doppler name; RESEND_API_KEY accepted as an alias.
	if resendKey := env.Get("RESEND_API", env.Get("RESEND_API_KEY", "")); resendKey != "" {
		mailer = mail.New(resendKey,
			env.Get("RESEND_FROM", "ItsBagelBot <no-reply@itsbagelbot.com>"),
			dashboardOrigin)
	} else {
		log.Warn("gift email disabled: RESEND_API not configured")
	}

	emailSubject := env.Get("NATS_INTERNAL_USERS_EMAIL_SUBJECT", "bagel.rpc.internal.users.email.get")
	notifier := rpc.NewGiftNotifier(nc, sendSubject, emailSubject, mailer, log.Named("gift"))
	billingSubject := env.Get("NATS_INTERNAL_BILLING_SUBJECT", "bagel.rpc.internal.billing.apply")
	billing := rpc.NewBillingApplier(nc, billingSubject)

	listenAddr := env.Get("LISTEN_ADDR", ":8080")
	handler := web.New(repo, web.Config{
		WebhookSecret: env.Get("TEBEX_WEBHOOK_SECRET", ""),
		NotifyGift:    notifier.Notify,
		ApplyBilling:  billing.Apply,
	}, log.Named("http"))

	httpServer := &http.Server{
		Addr:        listenAddr,
		Handler:     handler,
		ReadTimeout: 5 * time.Second,
		// net/http arms the write deadline when the request is read, not when
		// the handler returns, so this must outlast /drain's 10s sleep.
		WriteTimeout: 15 * time.Second,
	}

	log.Info("transactions service ready",
		zap.String("listen_addr", listenAddr),
		zap.Bool("tebex_webhook_configured", env.Get("TEBEX_WEBHOOK_SECRET", "") != ""),
		zap.Bool("tebex_checkout_configured", checkoutConfigured),
		zap.Bool("tebex_checkout_auth_configured", privateKey != ""),
		zap.Bool("tebex_checkout_username_configured", env.GetBool("TEBEX_INCLUDE_USERNAME", false)),
	)

	serveHTTP(ctx, httpServer, log)
}

// serveHTTP runs the server until ctx is cancelled or the listener fails,
// then drains in-flight requests before returning.
func serveHTTP(ctx context.Context, srv *http.Server, log *zap.Logger) {

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("transactions http server stopped", zap.Error(err))
		}
	}

	log.Info("transactions service shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("transactions http server shutdown failed", zap.Error(err))
	}
}
