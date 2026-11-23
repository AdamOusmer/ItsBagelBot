// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/users/ent/runtime"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/app/db/users/tokensign"
	"ItsBagelBot/internal/adminauth"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/data"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const (
	serviceName = "users"
	queueGroup  = "users-rpc"
)

const shutdownFlushBudget = 10 * time.Second

func ownedAccountLevels() []string {
	return []string{
		string(catalog.AccountsTotal), string(catalog.AccountsBotEnabled), string(catalog.AccountsOnboarded),
		string(catalog.AccountsPaidSource), string(catalog.AccountsCancelPending), string(catalog.AccountsPaymentFailed),
		string(catalog.AccountsLocale), string(catalog.AccountsPagesHidden), string(catalog.AccountsBySignupWeek),
		string(catalog.PaidBySignupWeek), string(catalog.AccountsDelegating),
		string(catalog.AccountsRuntime), string(catalog.AccountsInactive), string(catalog.DesktopGrants),
	}
}

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx := core.Log, core.Ctx

	client, dbPool, packer := openStore(ctx, core)
	defer func() { _ = client.Close() }()

	svcboot.FatalIf(log, bus.EnsureStreams(ctx, core.NATSURL, []bus.StreamSpec{bus.DataStream, bus.DataDeadLetterStream}, log),
		"failed to provision DATA streams")
	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()

	vc := svcboot.MustValkey(core)
	recorder := analytics.New(vc, log, nil)
	defer recorder.Close()

	h := bus.NewInvalidations(n.RPC, conf.CacheInvalidationPrefix.Get(), log)
	repo := repository.NewUsers(repository.UsersConfig{Client: client, Packer: packer, Pub: h.Publisher(n.Pub), App: core.NR, Log: log})
	repo.SetAnalytics(recorder)
	repo.SetAdminAuth(adminauth.New(n.RPC, adminauth.Config{AuthPrefix: env.Get("NATS_ADMIN_AUTH_SUBJECT_PREFIX", adminauth.DefaultAuthPrefix), AuditPrefix: env.Get("NATS_ADMIN_AUDIT_SUBJECT_PREFIX", adminauth.DefaultAuditPrefix)}))
	repo.SetDeviceAuthSigner(loadDeviceAuthSigner(log))
	levels := analytics.NewLevelSource(analytics.Owner{Service: serviceName, Owned: ownedAccountLevels(), Seed: repo.AnalyticsSeed})
	repo.SetInvalidationConn(n.RPC)
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushBudget)
		defer cancel()
		repo.Close(flushCtx)
	}()

	defer closeIntake() // stops intake before the repo flush above

	svcboot.FatalIf(log, h.Changes(ctx, n.Broadcast, data.SubjectUserChanged, data.SubjectUserDeleted), "failed to subscribe user changes")
	startConsumers(core, n, repo)
	wireErasure(ctx, repo, log)

	go expireSubscriptions(ctx, repo, log)
	go expirePremiumGrants(ctx, repo, log)
	go cleanupDeviceAuth(ctx, repo, log)
	go cleanupVoiceUsageReceipts(ctx, repo, log)
	go deliverAdminAudits(ctx, repo, log)

	svcboot.FatalIf(log, levels.Serve(bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log}), "failed to subscribe analytics levels rpc")

	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repo,
		Safety:    rpc.Safety{Store: vc, Rules: newSpeechSafety(log)},
	}
	subjects := subscribeRPCs(wiring, log)
	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: dbPool,
	})
	subjects.logReady(log)

	core.Await()
}

func speechLimitEnv(key string) string { return env.Get(key, "") }

func newSpeechSafety(log *zap.Logger) pkgvalkey.SpeechSafety {
	limits, err := pkgvalkey.ParseSpeechLimits(speechLimitEnv)
	svcboot.FatalIf(log, err, "invalid TTS safety limits")
	return pkgvalkey.NewSpeechSafety(limits)
}

func loadDeviceAuthSigner(log *zap.Logger) *tokensign.Signer {
	seedB64 := strings.TrimSpace(env.Get("DEVICE_AUTH_SIGNING_KEY", ""))
	kid := strings.TrimSpace(env.Get("DEVICE_AUTH_SIGNING_KID", ""))
	if seedB64 == "" || kid == "" {
		log.Warn("device authorization signing key not configured; exchange and refresh will fail closed")
		return nil
	}
	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil {
		log.Error("device authorization signing key is not valid base64", zap.Error(err))
		return nil
	}
	signer, err := tokensign.NewSigner(seed, kid)
	if err != nil {
		log.Error("device authorization signing key rejected", zap.Error(err))
		return nil
	}
	return signer
}

func openStore(ctx context.Context, core svcboot.Core) (*ent.Client, *sql.DB, *crypto.Crypto) {
	log := core.Log

	packer := crypto.MustFromEnv(log)

	driver := databoot.MustEntDriver(core)
	client := ent.NewClient(ent.Driver(driver))
	databoot.AutoMigrate(ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })
	return client, driver.DB(), packer
}

func startConsumers(core svcboot.Core, n svcboot.NATS, repo *repository.Users) {
	svcboot.FatalIf(core.Log, bus.Consume(core.Ctx, core.NR, n.Grouped, data.SubjectReprojectRequest, func(*bus.Message) error {
		return repo.Reproject(core.Ctx)
	}, core.Log), "failed to subscribe to reproject requests")
}

type rpcSubjects struct {
	dashboard  string
	admin      string
	billing    string
	projection string
	giveaway   string
	device     string
}

func (s rpcSubjects) logReady(log *zap.Logger) {
	log.Info("users service ready",
		zap.String("dashboard_prefix", s.dashboard),
		zap.String("admin_prefix", s.admin),
		zap.String("billing_subject", s.billing),
		zap.String("projection_subject", s.projection),
		zap.String("giveaway_prefix", s.giveaway),
		zap.String("device_prefix", s.device))
}

func subscribeRPCs(wiring rpc.Wiring, log *zap.Logger) rpcSubjects {

	s := rpcSubjects{
		dashboard:  env.Get("NATS_DASHBOARD_SUBJECT_PREFIX", "rpc.dashboard"),
		admin:      env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "rpc.admin.user"),
		billing:    conf.BillingApplySubject.Get(),
		projection: conf.ProjectionUsersSubject.Get(),
		giveaway:   env.Get("NATS_INTERNAL_USERS_GIVEAWAY_SUBJECT_PREFIX", "rpc.internal.users.giveaway"),
		device:     env.Get("NATS_DASHBOARD_DEVICE_SUBJECT_PREFIX", "rpc.dashboard.device"),
	}

	svcboot.FatalIf(log, rpc.SubscribeDashboard(wiring, s.dashboard), "failed to subscribe dashboard rpc")
	adminCfg := rpc.AdminConfig{
		AuthPrefix:         env.Get("NATS_ADMIN_AUTH_SUBJECT_PREFIX", adminauth.DefaultAuthPrefix),
		BotUserID:          strings.TrimSpace(env.Get("TWITCH_BOT_USER_ID", "")),
		Prefix:             s.admin,
		InternalGetSubject: conf.UsersGetSubject.Get(),
	}
	svcboot.FatalIf(log, rpc.SubscribeAdmin(wiring, adminCfg), "failed to subscribe admin rpc")
	svcboot.FatalIf(log, rpc.SubscribeBilling(wiring, s.billing), "failed to subscribe billing rpc")
	svcboot.FatalIf(log, rpc.SubscribeGiveaways(wiring, s.giveaway), "failed to subscribe giveaway rpc")

	svcboot.FatalIf(log, rpc.SubscribeLifecycle(wiring), "failed to subscribe lifecycle rpc")
	svcboot.FatalIf(log, rpc.SubscribeProjection(wiring, s.projection), "failed to subscribe projection rpc")
	svcboot.FatalIf(log, rpc.SubscribeEmail(wiring, conf.UsersEmailSubject.Get()),
		"failed to subscribe email rpc")
	svcboot.FatalIf(log, rpc.SubscribeTokens(wiring, conf.TokensSubjectPrefix.Get()),
		"failed to subscribe tokens rpc")
	svcboot.FatalIf(log, rpc.SubscribeCounts(wiring, env.Get("NATS_INTERNAL_USERS_COUNTS_SUBJECT", "rpc.internal.users.counts.get")),
		"failed to subscribe counts rpc")
	svcboot.FatalIf(log, rpc.SubscribeDelegation(wiring, env.Get("NATS_DELEGATION_SUBJECT_PREFIX", "rpc.delegation")),
		"failed to subscribe delegation rpc")
	svcboot.FatalIf(log, rpc.SubscribeAlpha(wiring, usersrpc.AlphaCheckSubject), "failed to subscribe alpha rpc")
	svcboot.FatalIf(log, rpc.SubscribeVoice(wiring, usersrpc.VoiceCheckSubject, usersrpc.VoiceRecordSubject), "failed to subscribe voice rpc")
	svcboot.FatalIf(log, rpc.SubscribeDeviceAuth(wiring, s.device), "failed to subscribe device authorization rpc")

	return s
}

func subscriptionSweepInterval() time.Duration {
	return env.GetDuration("USERS_SUBSCRIPTION_SWEEP_INTERVAL", 5*time.Minute)
}

func expireSubscriptions(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	const tebexGrace = 24 * time.Hour
	runPeriodicSweep(ctx, log, sweepSpec{
		interval:       subscriptionSweepInterval(),
		timeout:        30 * time.Second,
		errorMessage:   "failed to expire subscriptions",
		successMessage: "expired subscriptions",
		run: func(runCtx context.Context, now time.Time) (int, error) {
			return repo.ExpireSubscriptions(runCtx, now, tebexGrace)
		},
	})
}

func premiumGrantSweepInterval() time.Duration {
	return env.GetDuration("USERS_PREMIUM_GRANT_SWEEP_INTERVAL", 30*time.Second)
}

func expirePremiumGrants(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	runPeriodicSweep(ctx, log, sweepSpec{
		interval:       premiumGrantSweepInterval(),
		timeout:        10 * time.Second,
		errorMessage:   "failed to expire premium grants",
		successMessage: "expired premium grants",
		run:            repo.ExpirePremiumGrants,
	})
}

func deliverAdminAudits(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	spec := sweepSpec{
		interval:       env.GetDuration("USERS_ADMIN_AUDIT_DELIVERY_INTERVAL", 30*time.Second),
		timeout:        30 * time.Second,
		errorMessage:   "failed to deliver admin audit records",
		successMessage: "delivered admin audit records",
		run:            repo.DeliverAdminAudits,
	}
	runCtx, cancel := context.WithTimeout(ctx, spec.timeout)
	_, err := repo.DeliverAdminAudits(runCtx, time.Now().UTC())
	cancel()
	if err != nil {
		log.Error(spec.errorMessage, zap.Error(err))
	}
	runPeriodicSweep(ctx, log, spec)
}

func cleanupDeviceAuth(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	runPeriodicSweep(ctx, log, sweepSpec{
		interval:       env.GetDuration("USERS_DEVICE_AUTH_SWEEP_INTERVAL", 5*time.Minute),
		timeout:        10 * time.Second,
		errorMessage:   "failed to clean up device authorization",
		successMessage: "cleaned up device authorization",
		run:            repo.CleanupDeviceAuth,
	})
}

func cleanupVoiceUsageReceipts(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	runPeriodicSweep(ctx, log, sweepSpec{
		interval:       5 * time.Minute,
		timeout:        10 * time.Second,
		errorMessage:   "failed to clean up voice usage receipts",
		successMessage: "cleaned up voice usage receipts",
		run:            repo.CleanupVoiceUsageReceipts,
	})
}

type sweepSpec struct {
	interval       time.Duration
	timeout        time.Duration
	errorMessage   string
	successMessage string
	run            func(context.Context, time.Time) (int, error)
}

func runPeriodicSweep(ctx context.Context, log *zap.Logger, spec sweepSpec) {
	ticker := time.NewTicker(spec.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			spec.runOnce(ctx, log, now)
		}
	}
}

func (s sweepSpec) runOnce(ctx context.Context, log *zap.Logger, now time.Time) {
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	count, err := s.run(runCtx, now)
	cancel()
	if err != nil {
		log.Error(s.errorMessage, zap.Error(err))
		return
	}
	if count > 0 {
		log.Info(s.successMessage, zap.Int("count", count))
	}
}
