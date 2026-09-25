// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/users/ent/runtime"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName = "users"
	queueGroup  = "users-rpc"
)

const shutdownFlushBudget = 10 * time.Second

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

	repo := repository.NewUsers(client, packer, n.Pub, core.NR, log)
	repo.SetInvalidationPrefix(env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"))
	repo.SetInvalidationConn(n.RPC)
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushBudget)
		defer cancel()
		repo.Close(flushCtx)
	}()

	defer closeIntake() // stops intake before the repo flush above

	startConsumers(ctx, n, repo, log)

	go expireSubscriptions(ctx, repo, log)
	go expirePremiumGrants(ctx, repo, log)

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

func openStore(ctx context.Context, core svcboot.Core) (*ent.Client, *sql.DB, *crypto.Crypto) {
	log := core.Log

	keysetJSON, err := os.ReadFile(env.MustGet("TINK_KEYSET_PATH"))
	svcboot.FatalIf(log, err, "failed to read tink keyset")

	packer, err := crypto.NewCrypto(keysetJSON)
	svcboot.FatalIf(log, err, "failed to initialize crypto")

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
}

func (s rpcSubjects) logReady(log *zap.Logger) {
	log.Info("users service ready",
		zap.String("dashboard_prefix", s.dashboard),
		zap.String("admin_prefix", s.admin),
		zap.String("billing_subject", s.billing),
		zap.String("projection_subject", s.projection),
		zap.String("giveaway_prefix", s.giveaway))
}

func subscribeRPCs(ctx context.Context, wiring rpc.Wiring, client *ent.Client, log *zap.Logger) rpcSubjects {
	invalidationPrefix := env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate")

	s := rpcSubjects{
		dashboard:  env.Get("NATS_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.dashboard"),
		admin:      env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user"),
		billing:    env.Get("NATS_INTERNAL_BILLING_SUBJECT", "bagel.rpc.internal.billing.apply"),
		projection: env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		giveaway:   env.Get("NATS_INTERNAL_USERS_GIVEAWAY_SUBJECT_PREFIX", "bagel.rpc.internal.users.giveaway"),
	}

	svcboot.FatalIf(log, rpc.SubscribeDashboard(wiring, s.dashboard, invalidationPrefix), "failed to subscribe dashboard rpc")
	adminCfg := rpc.AdminConfig{
		Prefix:             s.admin,
		InternalGetSubject: env.Get("NATS_INTERNAL_USERS_GET_SUBJECT", "bagel.rpc.internal.users.get"),
		InvalidationPrefix: invalidationPrefix,
	}
	svcboot.FatalIf(log, rpc.SubscribeAdmin(wiring, client, adminCfg), "failed to subscribe admin rpc")
	svcboot.FatalIf(log, rpc.SubscribeBilling(wiring, s.billing, invalidationPrefix), "failed to subscribe billing rpc")
	svcboot.FatalIf(log, rpc.SubscribeGiveaways(wiring, s.giveaway, invalidationPrefix), "failed to subscribe giveaway rpc")

	seedBootstrapStaff(ctx, client, log)
	authPrefix := env.Get("NATS_ADMIN_AUTH_SUBJECT_PREFIX", "bagel.rpc.admin.user.auth")
	auditPrefix := env.Get("NATS_ADMIN_AUDIT_SUBJECT_PREFIX", "bagel.rpc.admin.user.audit")
	svcboot.FatalIf(log, rpc.SubscribeAdminAuth(wiring, client, authPrefix, auditPrefix), "failed to subscribe admin auth rpc")

	svcboot.FatalIf(log, rpc.SubscribeProjection(wiring, s.projection), "failed to subscribe projection rpc")
	svcboot.FatalIf(log, rpc.SubscribeEmail(wiring, env.Get("NATS_INTERNAL_USERS_EMAIL_SUBJECT", "bagel.rpc.internal.users.email.get")),
		"failed to subscribe email rpc")
	svcboot.FatalIf(log, rpc.SubscribeTokens(wiring, env.Get("NATS_INTERNAL_TOKENS_SUBJECT_PREFIX", "bagel.rpc.internal.tokens")),
		"failed to subscribe tokens rpc")
	svcboot.FatalIf(log, rpc.SubscribeCounts(wiring, env.Get("NATS_INTERNAL_USERS_COUNTS_SUBJECT", "bagel.rpc.internal.users.counts.get")),
		"failed to subscribe counts rpc")
	svcboot.FatalIf(log, rpc.SubscribeDelegation(wiring, env.Get("NATS_DELEGATION_SUBJECT_PREFIX", "bagel.rpc.delegation"), invalidationPrefix),
		"failed to subscribe delegation rpc")

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
