// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/db/transactions/ent/runtime"
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

	driver := databoot.MustEntDriver(log, "bagel_transactions")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	repo := repository.NewTransactions(client)

	// RPC-plane connection (TRANSACTIONS_RPC account): answers the checkout
	// basket verb and issues the recipient-lookup / gift-notification requests.
	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()

	dashboardOrigin := env.Get("DASHBOARD_ORIGIN", "https://dashboard.itsbagelbot.com")
	checkoutConfigured, checkoutAuth := setupCheckout(nc, core.NR, dashboardOrigin, log)

	sendSubject := env.Get("NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.admin.notifications") + ".send"
	mailer := newMailer(dashboardOrigin, log)

	emailSubject := env.Get("NATS_INTERNAL_USERS_EMAIL_SUBJECT", "bagel.rpc.internal.users.email.get")
	notifier := rpc.NewGiftNotifier(nc, sendSubject, emailSubject, mailer, log.Named("gift"))
	billingSubject := env.Get("NATS_INTERNAL_BILLING_SUBJECT", "bagel.rpc.internal.billing.apply")
	billing := rpc.NewBillingApplier(nc, billingSubject)

	// The Set is built here rather than served by svcboot.ServeHealth because
	// this service owns its own HTTP server: the same handler that answers the
	// Tebex webhook serves /status, so the Set has to be embedded in it.
	//
	// This Set is the whole billing vertical's answer. Billing is checked
	// independently of the projector by design: the projector's /db does not
	// probe transactions, and transactions answers for billing alone, so there
	// is no bus.HealthProbe fan-out here — the two verticals fail separately
	// and report separately. Tebex is deliberately not a check either; see
	// web.Server.tebexReachable for why an inbound validation endpoint is not
	// an outbound dependency.
	healthSet := svcboot.NewHealthSet(svcboot.Health{
		Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
	}, health.Degrades(db.HealthCheck("mysql", driver.DB())))

	handler := web.New(repo, web.Config{
		WebhookSecret: env.Get("TEBEX_WEBHOOK_SECRET", ""),
		Health:        healthSet,
		NotifyGift:    notifier.Notify,
		ApplyBilling:  billing.Apply,
		App:           core.NR,
	}, log.Named("http"))

	httpServer := &http.Server{
		Addr:        core.ListenAddr,
		Handler:     handler,
		ReadTimeout: 5 * time.Second,
		// net/http arms the write deadline when the request is read, not when
		// the handler returns, so this must outlast /drain's 10s sleep.
		WriteTimeout: 15 * time.Second,
	}

	// TLS is opt-in via the cert-manager-issued fleet CA cert (see
	// deploy/infra/pki/certificates.yaml, transactions-tls), mirroring
	// console-dashboard. Both or neither: unset stays plaintext exactly as
	// before, so this can land before the cert and the traefik ServersTransport
	// exist. A mismatched pair is a config error, not a runtime fallback.
	tlsPair, err := tlsenv.PairFromEnv("TLS_CERT_FILE", "TLS_KEY_FILE")
	if err != nil {
		log.Fatal("transactions tls misconfigured", zap.Error(err))
	}

	log.Info("transactions service ready",
		zap.String("listen_addr", core.ListenAddr),
		zap.Bool("tls_enabled", tlsPair.Configured()),
		zap.Bool("tebex_webhook_configured", env.Get("TEBEX_WEBHOOK_SECRET", "") != ""),
		zap.Bool("tebex_checkout_configured", checkoutConfigured),
		zap.Bool("tebex_checkout_auth_configured", checkoutAuth),
		zap.Bool("tebex_checkout_username_configured", env.GetBool("TEBEX_INCLUDE_USERNAME", false)),
	)

	serveHTTP(core.Ctx, listener{srv: httpServer, cert: tlsPair}, log)
}

// setupCheckout registers the dashboard basket_create RPC. Optional: without
// the Tebex Headless credentials the service stays webhook-only, exactly as
// before. Returns whether checkout is live and whether an API private key is
// configured, both reported in the ready log line.
func setupCheckout(nc *nats.Conn, nrApp *newrelic.Application, dashboardOrigin string, log *zap.Logger) (configured, auth bool) {

	// TEBEX_HEADLESS_TOKEN is the legacy name for the same webstore public token.
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

	userGetSubject := env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user") + ".get"
	prefix := env.Get("NATS_TRANSACTIONS_SUBJECT_PREFIX", "bagel.rpc.transactions")
	if err := rpc.SubscribeCheckout(
		rpc.CheckoutRuntime{NC: nc, App: nrApp, Log: log},
		tebexClient,
		rpc.CheckoutConfig{Prefix: prefix, UserGetSubject: userGetSubject, QueueGroup: queueGroup},
	); err != nil {
		log.Fatal("failed to subscribe checkout rpc", zap.Error(err))
	}

	return true, privateKey != ""
}

// newMailer builds the Resend gift-email channel. Optional: without the API
// key the notifier keeps sending the in-app notification only, exactly as
// before.
func newMailer(dashboardOrigin string, log *zap.Logger) *mail.Mailer {

	// RESEND_API is the Doppler name; RESEND_API_KEY accepted as an alias.
	resendKey := env.Get("RESEND_API", env.Get("RESEND_API_KEY", ""))
	if resendKey == "" {
		log.Warn("gift email disabled: RESEND_API not configured")
		return nil
	}

	return mail.New(resendKey,
		env.Get("RESEND_FROM", "ItsBagelBot <no-reply@itsbagelbot.com>"),
		dashboardOrigin)
}

// serveHTTP runs the server until ctx is cancelled or the listener fails,
// then drains in-flight requests before returning. certFile/keyFile serve TLS
// when both are set; empty (the default) keeps plaintext HTTP.
// listener carries the server together with the cert pair it should present.
// They are decided together and never travel apart, so they are passed together.
type listener struct {
	srv  *http.Server
	cert tlsenv.Pair
}

// serve blocks on the underlying server, choosing TLS when a pair was configured.
func (l listener) serve() error {
	if l.cert.Configured() {
		return l.srv.ListenAndServeTLS(l.cert.CertFile(), l.cert.KeyFile())
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
