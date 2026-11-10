// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/users/ent/runtime"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rollup"
	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/app/db/users/tokensign"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

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
		string(catalog.PaidBySignupWeek),
	}
}

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx := core.Log, core.Ctx

	client, dbPool, packer := openStore(ctx, core)
	defer func() { _ = client.Close() }()

	svcboot.FatalIf(log, bus.EnsureStreams(ctx, core.NATSURL, []bus.StreamSpec{bus.BagelDataStream, bus.BagelDeadLetterStream}, log),
		"failed to provision BAGEL_DATA streams")
	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()

	vc := svcboot.MustValkey(core)
	recorder := analytics.New(vc, log, nil)
	defer recorder.Close()

	repo := repository.NewUsers(client, packer, n.Pub, core.NR, log)
	repo.SetAnalytics(recorder)
	repo.SetDeviceAuthSigner(loadDeviceAuthSigner(log))
	go recorder.ReconcileDaily(ctx, analytics.Owner{Service: serviceName, Owned: ownedAccountLevels(), Seed: repo.AnalyticsSeed})
	repo.SetInvalidationPrefix(conf.CacheInvalidationPrefix.Get())
	repo.SetInvalidationConn(n.RPC)
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushBudget)
		defer cancel()
		repo.Close(flushCtx)
	}()

	defer closeIntake() // stops intake before the repo flush above

	startConsumers(ctx, n, repo, log)
	wireErasure(ctx, repo, log)

	go expireSubscriptions(ctx, repo, log)
	go expirePremiumGrants(ctx, repo, log)
	go cleanupDeviceAuth(ctx, repo, log)
	go rollupAnalytics(ctx, rollup.New(vc, repo), log)

	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repo,
	}
	subjects := subscribeRPCs(ctx, wiring, client, log)
	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: dbPool,
	})
	subjects.logReady(log)

	core.Await()
}

// loadDeviceAuthSigner reads the Ed25519 seed from env/Doppler. A missing or
// malformed key fails closed: device authorization refuses exchange/refresh,
// but every other Users RPC keeps running.
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

	driver := databoot.MustEntDriver(core, "bagel_users")
	client := ent.NewClient(ent.Driver(driver))
	databoot.AutoMigrate(ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })
	return client, driver.DB(), packer
}

func startConsumers(ctx context.Context, n svcboot.NATS, repo *repository.Users, log *zap.Logger) {
	svcboot.FatalIf(log, bus.Consume(ctx, nil, n.Broadcast, data.SubjectUserChanged, consumers.OnChangeInvalidate(changedUserID, repo.Invalidate), log),
		"failed to subscribe to user changes")

	svcboot.FatalIf(log, bus.Consume(ctx, nil, n.Grouped, data.SubjectReprojectRequest, func(*bus.Message) error {
		return repo.Reproject(ctx)
	}, log), "failed to subscribe to reproject requests")
}

func changedUserID(dto data.UserChangedDTO) uint64 { return dto.UserID }

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

func subscribeRPCs(ctx context.Context, wiring rpc.Wiring, client *ent.Client, log *zap.Logger) rpcSubjects {
	invalidationPrefix := conf.CacheInvalidationPrefix.Get()

	s := rpcSubjects{
		dashboard:  env.Get("NATS_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.dashboard"),
		admin:      env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user"),
		billing:    conf.BillingApplySubject.Get(),
		projection: conf.ProjectionUsersSubject.Get(),
		giveaway:   env.Get("NATS_INTERNAL_USERS_GIVEAWAY_SUBJECT_PREFIX", "bagel.rpc.internal.users.giveaway"),
		device:     env.Get("NATS_DASHBOARD_DEVICE_SUBJECT_PREFIX", "bagel.rpc.dashboard.device"),
	}

	svcboot.FatalIf(log, rpc.SubscribeDashboard(wiring, s.dashboard, invalidationPrefix), "failed to subscribe dashboard rpc")
	adminCfg := rpc.AdminConfig{
		BotUserID:          strings.TrimSpace(env.Get("TWITCH_BOT_USER_ID", "")),
		Prefix:             s.admin,
		InternalGetSubject: conf.UsersGetSubject.Get(),
		InvalidationPrefix: invalidationPrefix,
	}
	svcboot.FatalIf(log, rpc.SubscribeAdmin(wiring, client, adminCfg), "failed to subscribe admin rpc")
	svcboot.FatalIf(log, rpc.SubscribeBilling(wiring, s.billing, invalidationPrefix), "failed to subscribe billing rpc")
	svcboot.FatalIf(log, rpc.SubscribeGiveaways(wiring, s.giveaway, invalidationPrefix), "failed to subscribe giveaway rpc")

	seedBootstrapStaff(ctx, client, log)
	authPrefix := env.Get("NATS_ADMIN_AUTH_SUBJECT_PREFIX", "bagel.rpc.admin.user.auth")
	auditPrefix := env.Get("NATS_ADMIN_AUDIT_SUBJECT_PREFIX", "bagel.rpc.admin.user.audit")
	svcboot.FatalIf(log, rpc.SubscribeAdminAuth(wiring, client, authPrefix, auditPrefix), "failed to subscribe admin auth rpc")

	svcboot.FatalIf(log, rpc.SubscribeLifecycle(wiring), "failed to subscribe lifecycle rpc")
	svcboot.FatalIf(log, rpc.SubscribeProjection(wiring, s.projection), "failed to subscribe projection rpc")
	svcboot.FatalIf(log, rpc.SubscribeEmail(wiring, conf.UsersEmailSubject.Get()),
		"failed to subscribe email rpc")
	svcboot.FatalIf(log, rpc.SubscribeTokens(wiring, conf.TokensSubjectPrefix.Get()),
		"failed to subscribe tokens rpc")
	svcboot.FatalIf(log, rpc.SubscribeCounts(wiring, env.Get("NATS_INTERNAL_USERS_COUNTS_SUBJECT", "bagel.rpc.internal.users.counts.get")),
		"failed to subscribe counts rpc")
	svcboot.FatalIf(log, rpc.SubscribeDelegation(wiring, env.Get("NATS_DELEGATION_SUBJECT_PREFIX", "bagel.rpc.delegation"), invalidationPrefix),
		"failed to subscribe delegation rpc")
	svcboot.FatalIf(log, rpc.SubscribeDeviceAuth(wiring, s.device), "failed to subscribe device authorization rpc")

	return s
}

func seedBootstrapStaff(ctx context.Context, client *ent.Client, log *zap.Logger) {
	owners := parseIDs(env.Get("OWNER_BOOTSTRAP_IDS", "804932984"))
	admins := parseIDs(env.Get("ADMIN_BOOTSTRAP_IDS", ""))
	if len(owners) == 0 && len(admins) == 0 {
		return
	}
	svcboot.FatalIf(log, rpc.SeedStaff(ctx, client, rpc.StaffSeed{Owners: owners, Admins: admins}, log),
		"failed to seed bootstrap staff")
}

func subscriptionSweepInterval() time.Duration {
	return env.GetDuration("USERS_SUBSCRIPTION_SWEEP_INTERVAL", 5*time.Minute)
}

func expireSubscriptions(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	const tebexGrace = 24 * time.Hour
	runPeriodicSweep(ctx, log, subscriptionSweepInterval(), 30*time.Second,
		"failed to expire subscriptions", "expired subscriptions",
		func(runCtx context.Context, now time.Time) (int, error) {
			return repo.ExpireSubscriptions(runCtx, now, tebexGrace)
		})
}

func rollupAnalytics(ctx context.Context, r *rollup.Rollup, log *zap.Logger) {
	runPeriodicSweep(ctx, log, env.GetDuration("USERS_ANALYTICS_ROLLUP_INTERVAL", 15*time.Minute), 2*time.Minute,
		"failed to roll up analytics", "rolled up analytics", r.Run)
}

func premiumGrantSweepInterval() time.Duration {
	return env.GetDuration("USERS_PREMIUM_GRANT_SWEEP_INTERVAL", 30*time.Second)
}

func expirePremiumGrants(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	runPeriodicSweep(ctx, log, premiumGrantSweepInterval(), 10*time.Second,
		"failed to expire premium grants", "expired premium grants",
		func(runCtx context.Context, now time.Time) (int, error) {
			return repo.ExpirePremiumGrants(runCtx, now)
		})
}

func cleanupDeviceAuth(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	interval := env.GetDuration("USERS_DEVICE_AUTH_SWEEP_INTERVAL", 5*time.Minute)
	runPeriodicSweep(ctx, log, interval, 10*time.Second,
		"failed to clean up device authorization", "cleaned up device authorization",
		repo.CleanupDeviceAuth)
}

func runPeriodicSweep(ctx context.Context, log *zap.Logger, interval, timeout time.Duration, errorMessage, successMessage string, sweep func(context.Context, time.Time) (int, error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, timeout)
			count, err := sweep(runCtx, now)
			cancel()
			if err != nil {
				log.Error(errorMessage, zap.Error(err))
			} else if count > 0 {
				log.Info(successMessage, zap.Int("count", count))
			}
		}
	}
}

func parseIDs(csv string) []uint64 {
	var out []uint64
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := strconv.ParseUint(part, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}
