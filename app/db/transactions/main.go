// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/transactions/ent/runtime"
	giveawayengine "ItsBagelBot/app/db/transactions/giveaway"
	"ItsBagelBot/app/db/transactions/mail"
	"ItsBagelBot/app/db/transactions/repository"
	"ItsBagelBot/app/db/transactions/rpc"
	"ItsBagelBot/app/db/transactions/tebex"
	"ItsBagelBot/app/db/transactions/web"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"
	"ItsBagelBot/pkg/tlsenv"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const (
	serviceName = "transactions"
	queueGroup  = "transactions-rpc"
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, "bagel_transactions")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	repo := repository.NewTransactions(client)

	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()

	dashboardOrigin := env.Get("DASHBOARD_ORIGIN", "https://dashboard.itsbagelbot.com")
	checkoutConfigured, checkoutAuth := setupCheckout(checkoutRuntime{nc: nc, db: client, nrApp: core.NR, dashboardOrigin: dashboardOrigin, log: log})

	sendSubject := env.Get("NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.admin.notifications") + ".send"
	mailer := newMailer(dashboardOrigin, log)

	emailSubject := env.Get("NATS_INTERNAL_USERS_EMAIL_SUBJECT", "bagel.rpc.internal.users.email.get")
	notifier := rpc.NewGiftNotifier(bus.RPCWiring{NC: nc, Log: log.Named("gift")}, rpc.GiftNotifierConfig{SendSubject: sendSubject, EmailSubject: emailSubject, Mailer: mailer})
	billingSubject := env.Get("NATS_INTERNAL_BILLING_SUBJECT", "bagel.rpc.internal.billing.apply")
	billing := rpc.NewBillingApplier(nc, billingSubject)

	giveawayConfig := giveawayengine.ConfigFromEnv()
	usersGiveaways := rpc.NewUsersGiveawayClient(nc)
	giveawayStore := giveawayengine.NewStore(client)
	giveawayProvider, providerAvailable := newGiveawayProvider(giveawayConfig, log.Named("giveaways"))
	if !providerAvailable {
		giveawayConfig.ProviderMutations = false
	}
	giveawayEngine := newGiveawayEngine(giveawayRuntimeConfig{Store: giveawayStore, Users: usersGiveaways, Mailer: mailer, Provider: giveawayProvider, Config: giveawayConfig})
	giveawayRPC := rpc.NewGiveawayRPC(rpc.GiveawayRPCConfig{Store: giveawayStore, DB: client, Users: usersGiveaways, Config: giveawayConfig, RulesVersion: env.Get("GIVEAWAYS_RULES_VERSION", "premium-giveaway-v1"), Log: log.Named("giveaways")})
	if err := rpc.SubscribeGiveaways(bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log.Named("giveaways")}, giveawayRPC); err != nil {
		log.Fatal("failed to subscribe giveaways rpc", zap.Error(err))
	}
	go func() {
		if err := giveawayEngine.Run(core.Ctx); err != nil && core.Ctx.Err() == nil {
			log.Warn("giveaway engine stopped", zap.Error(err))
		}
	}()

	healthSet := svcboot.NewHealthSet(svcboot.Health{
		Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
	}, health.Degrades(db.HealthCheck("mysql", driver.DB())))

	handler := web.New(repo, web.Config{
		WebhookSecret: env.Get("TEBEX_WEBHOOK_SECRET", ""),
		Health:        healthSet,
		NotifyGift:    notifier.Notify,
		ApplyBilling:  billing.Apply,
		RecordBillingIncident: func(ctx context.Context, incident web.BillingIncident) error {
			return recordGiveawayBillingIncident(ctx, client, incident)
		},
		App: core.NR,
	}, log.Named("http"))

	httpServer := &http.Server{
		Addr:        core.ListenAddr,
		Handler:     handler,
		ReadTimeout: 5 * time.Second,
		// Must outlast /drain's 10s sleep: the deadline arms at request read.
		WriteTimeout: 15 * time.Second,
	}

	tlsPair, err := tlsenv.PairFromEnv("TLS_CERT_FILE", "TLS_KEY_FILE")
	if err != nil {
		log.Fatal("transactions tls misconfigured", zap.Error(err))
	}

	httpServer.TLSConfig, err = tlsPair.ServerConfig()
	if err != nil {
		log.Fatal("transactions tls cert unusable", zap.Error(err))
	}

	log.Info("transactions service ready",
		zap.String("listen_addr", core.ListenAddr),
		zap.Bool("tls_enabled", tlsPair.Configured()),
		zap.Bool("tebex_webhook_configured", env.Get("TEBEX_WEBHOOK_SECRET", "") != ""),
		zap.Bool("tebex_checkout_configured", checkoutConfigured),
		zap.Bool("tebex_checkout_auth_configured", checkoutAuth),
		zap.Bool("tebex_checkout_username_configured", env.GetBool("TEBEX_INCLUDE_USERNAME", false)),
		zap.Bool("giveaways_new_awards_enabled", giveawayConfig.NewAwardsEnabled),
		zap.Bool("giveaways_interval_rule_verified", giveawayConfig.IntervalRuleVerified),
		zap.Bool("giveaways_provider_mutations_enabled", giveawayConfig.CanMutateProvider()),
	)

	serveHTTP(core.Ctx, listener{srv: httpServer}, log)
}

type checkoutRuntime struct {
	nc              *nats.Conn
	db              *ent.Client
	nrApp           *newrelic.Application
	dashboardOrigin string
	log             *zap.Logger
}

func setupCheckout(runtime checkoutRuntime) (configured, auth bool) {
	nc, db, nrApp, dashboardOrigin, log := runtime.nc, runtime.db, runtime.nrApp, runtime.dashboardOrigin, runtime.log

	webstoreToken := env.Get("TEBEX_WEBSTORE_TOKEN", env.Get("TEBEX_HEADLESS_TOKEN", ""))
	privateKey := env.Get("TEBEX_PRIVATE_KEY", env.Get("TEBEX_SECRET_KEY", env.Get("TEBEX_API_PRIVATE_KEY", "")))
	packageID := env.GetInt("TEBEX_PACKAGE_ID", 0)
	if webstoreToken == "" || packageID <= 0 {
		log.Warn("tebex checkout rpc disabled: TEBEX_WEBSTORE_TOKEN / TEBEX_PACKAGE_ID not configured")
		return false, privateKey != ""
	}

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

	userGetSubject := env.Get("NATS_INTERNAL_USERS_GET_SUBJECT", "bagel.rpc.internal.users.get")
	prefix := env.Get("NATS_TRANSACTIONS_SUBJECT_PREFIX", "bagel.rpc.transactions")
	usersGiveaways := rpc.NewUsersGiveawayClient(nc)
	guard := rpc.NewCheckoutGuard(db, usersGiveaways)
	if err := rpc.SubscribeCheckout(
		bus.RPCWiring{NC: nc, App: nrApp, Queue: queueGroup, Log: log},
		tebexClient,
		rpc.CheckoutConfig{Prefix: prefix, UserGetSubject: userGetSubject, Guard: guard},
	); err != nil {
		log.Fatal("failed to subscribe checkout rpc", zap.Error(err))
	}

	return true, privateKey != ""
}

func newMailer(dashboardOrigin string, log *zap.Logger) *mail.Mailer {

	resendKey := env.Get("RESEND_API", env.Get("RESEND_API_KEY", ""))
	if resendKey == "" {
		log.Warn("gift email disabled: RESEND_API not configured")
		return nil
	}

	return mail.New(resendKey,
		env.Get("RESEND_FROM", "ItsBagelBot <no-reply@itsbagelbot.com>"),
		dashboardOrigin)
}

type listener struct {
	srv *http.Server
}

// Empty file names keep per-handshake cert reloads; naming them snapshots the cert at boot.
func (l listener) serve() error {
	if l.srv.TLSConfig != nil {
		return l.srv.ListenAndServeTLS("", "")
	}
	return l.srv.ListenAndServe()
}

func serveHTTP(ctx context.Context, l listener, log *zap.Logger) {
	srv := l.srv

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- l.serve()
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
