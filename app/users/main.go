// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"ItsBagelBot/app/users/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/users/ent/runtime"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/app/users/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"github.com/AdamOusmer/recipes/runtime"
	"github.com/AdamOusmer/recipes/svc/zkek2"
	"github.com/nats-io/nats.go"

	"go.uber.org/zap"
)

const serviceName = "users"

// fatalIf aborts startup on err: the users service cannot run degraded without
// any of its core dependencies, so a failed step must crash the pod.
func fatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

func main() {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	fatalIf(log, err, "failed to start new relic")
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, packer := openStore(ctx, log)
	defer func() { _ = client.Close() }()

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	nc, k, grantsDenied, pub := connectBus(ctx, natsURL, log)
	defer nc.Close()
	defer func() { _ = pub.Close() }()

	repo := repository.NewUsers(client, packer, pub)
	defer repo.Close()

	closeConsumers := startConsumers(ctx, natsURL, k, repo, log)
	defer closeConsumers()

	go expireSubscriptions(ctx, repo, log)

	wiring := rpc.Wiring{NC: nc, Repo: repo, App: nrApp, Queue: "users-rpc", Log: log}
	subjects := subscribeRPCs(ctx, wiring, k, client, log)
	fatalIf(log, bus.SubscribeRPCHealth(nc, serviceName, "users-rpc"), "failed to subscribe rpc health")

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), serviceName,
		health.Bool("nats", nc.IsConnected),
		health.Bool("nats_grants", func() bool { return !grantsDenied.Load() }),
	)
	subjects.logReady(log)

	<-ctx.Done()

	log.Info("users service shutting down")
}

// openStore reads the encryption keyset, opens the database, runs migrations,
// and returns the ent client and the field-crypto packer.
func openStore(ctx context.Context, log *zap.Logger) (*ent.Client, *crypto.Crypto) {
	keysetJSON, err := os.ReadFile(env.MustGet("TINK_KEYSET_PATH"))
	fatalIf(log, err, "failed to read tink keyset")

	packer, err := crypto.NewCrypto(keysetJSON)
	fatalIf(log, err, "failed to initialize crypto")

	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", "bagel_users"),
	})
	fatalIf(log, err, "failed to open database")

	client := ent.NewClient(ent.Driver(driver))
	if env.GetBool("DB_AUTO_MIGRATE", true) {
		fatalIf(log, client.Schema.Create(ctx), "failed to run migrations")
	}
	return client, packer
}

// connectBus dials the RPC and BUS-plane connections, binds users's recipes
// binding, wires its permission-violation Watchdog, preflights the JetStream
// grants it declares, then reconciles the BAGEL_DATA stream users owns.
// TWITCH_INGRESS is owned by sesame; keeping ownership separate is what lets
// NATS scope stream-management ACLs.
func connectBus(ctx context.Context, natsURL string, log *zap.Logger) (*nats.Conn, *zkek2.K, *atomic.Bool, bus.Publisher) {
	nc, err := bus.Connect(bus.RPCURL(natsURL), serviceName)
	fatalIf(log, err, "failed to connect to nats")

	busConn, err := bus.ConnectBus(natsURL, serviceName)
	fatalIf(log, err, "failed to connect bus-plane nats")

	k, err := zkek2.Up(zkek2.U{Bus: busConn, Rpc: nc})
	fatalIf(log, err, "failed to bind users's recipes binding")

	// grantsDenied flips true the first time the NATS server reports users's
	// BUS account denied a permission its manifest declares (see
	// runtime.Watchdog); the returned pointer backs the nats_grants health
	// check.
	grantsDenied := &atomic.Bool{}
	watchdog := runtime.NewWatchdog(k.M(), func(subject, canonical string, err error) {
		log.Error("nats permission violation",
			zap.String("subject", subject), zap.String("canonical", canonical), zap.Error(err))
	}, func() { grantsDenied.Store(true) })
	bus.GuardConnection(busConn, watchdog.Handler())

	fatalIf(log, runtime.PreflightStreams(ctx, busConn, k.Expectations(), 0),
		"recipes preflight failed: missing jetstream grant(s)")

	fatalIf(log, bus.EnsureStreams(ctx, natsURL, k.Z5G2Z(), log), "failed to provision BAGEL_DATA stream")

	pub, err := bus.NewPublisher(natsURL, log)
	fatalIf(log, err, "failed to connect publisher")

	return nc, k, grantsDenied, pub
}

// startConsumers wires the two event-plane consumers: a groupless broadcast
// subscriber that drops each instance's cached view on any user change, and a
// durable-group subscriber where exactly one instance answers a reproject by
// replaying the table. The returned cleanup closes both subscribers.
func startConsumers(ctx context.Context, natsURL string, k *zkek2.K, repo *repository.Users, log *zap.Logger) func() {
	broadcast, err := bus.NewSubscriber(natsURL, "", log)
	fatalIf(log, err, "failed to connect broadcast subscriber")
	fatalIf(log, bus.Consume(ctx, nil, broadcast, k.ZUHPS().Subject, invalidateOnUserChange(repo), log),
		"failed to subscribe to user changes")

	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	fatalIf(log, err, "failed to connect group subscriber")
	fatalIf(log, bus.Consume(ctx, nil, grouped, k.Z7NB4().Subject, func(*bus.Message) error {
		return repo.Reproject(ctx)
	}, log), "failed to subscribe to reproject requests")

	return func() {
		_ = grouped.Close()
		_ = broadcast.Close()
	}
}

// invalidateOnUserChange drops the local cached view for a changed user.
func invalidateOnUserChange(repo *repository.Users) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.UserChangedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}
		repo.Invalidate(dto.UserID)
		return nil
	}
}

// rpcSubjects records the subjects the RPC surfaces bound to, for the ready log.
type rpcSubjects struct {
	dashboard  string
	admin      string
	billing    string
	projection string
}

func (s rpcSubjects) logReady(log *zap.Logger) {
	log.Info("users service ready",
		zap.String("dashboard_prefix", s.dashboard),
		zap.String("admin_prefix", s.admin),
		zap.String("billing_subject", s.billing),
		zap.String("projection_subject", s.projection))
}

// subscribeRPCs binds every RPC surface the users service serves and seeds the
// bootstrap staff, returning the subjects for the ready log.
func subscribeRPCs(ctx context.Context, wiring rpc.Wiring, k *zkek2.K, client *ent.Client, log *zap.Logger) rpcSubjects {
	// invalidationPrefix recovers "bagel.cache.invalidate" from one of users's
	// four separate cache-invalidate publish grants (delegation/grant/locale/
	// status): the binding grants each concrete subject rather than a shared
	// parent wildcard.
	invalidationPrefix := strings.TrimSuffix(k.ZVBBD(), ".delegation")

	s := rpcSubjects{
		dashboard:  strings.TrimSuffix(k.ZBPQ3(), ".>"),
		admin:      strings.TrimSuffix(k.ZU2R3(), ".>"),
		billing:    k.Z7G7M(),
		projection: k.ZG2UC(),
	}

	fatalIf(log, rpc.SubscribeDashboard(wiring, s.dashboard, invalidationPrefix), "failed to subscribe dashboard rpc")
	fatalIf(log, rpc.SubscribeAdmin(wiring, s.admin, invalidationPrefix), "failed to subscribe admin rpc")
	fatalIf(log, rpc.SubscribeBilling(wiring, s.billing, invalidationPrefix), "failed to subscribe billing rpc")

	// Admin authorization + audit ride sub-paths of the same admin.user.>
	// grant (ZU2R3): auth.check / auth.* / audit.* under it, in place of the
	// old static env allowlist. Seed the bootstrap owners/admins so a fresh DB
	// is never locked out.
	seedBootstrapStaff(ctx, client, log)
	authPrefix := s.admin + ".auth"
	auditPrefix := s.admin + ".audit"
	fatalIf(log, rpc.SubscribeAdminAuth(wiring, client, authPrefix, auditPrefix), "failed to subscribe admin auth rpc")

	fatalIf(log, rpc.SubscribeProjection(wiring, s.projection), "failed to subscribe projection rpc")
	fatalIf(log, rpc.SubscribeEmail(wiring, k.Z2R6C()), "failed to subscribe email rpc")
	fatalIf(log, rpc.SubscribeTokens(wiring, strings.TrimSuffix(k.ZBPE6(), ".>")), "failed to subscribe tokens rpc")
	fatalIf(log, rpc.SubscribeDelegation(wiring, strings.TrimSuffix(k.ZVBVP(), ".>"), invalidationPrefix),
		"failed to subscribe delegation rpc")

	return s
}

// seedBootstrapStaff guarantees the configured owners/admins exist so a fresh
// DB is never locked out. The owner default is itsmavey's Twitch id; override
// via OWNER_BOOTSTRAP_IDS.
func seedBootstrapStaff(ctx context.Context, client *ent.Client, log *zap.Logger) {
	owners := parseIDs(env.Get("OWNER_BOOTSTRAP_IDS", "804932984"))
	admins := parseIDs(env.Get("ADMIN_BOOTSTRAP_IDS", ""))
	if len(owners) == 0 && len(admins) == 0 {
		return
	}
	fatalIf(log, rpc.SeedStaff(ctx, client, rpc.StaffSeed{Owners: owners, Admins: admins}, log),
		"failed to seed bootstrap staff")
}

func expireSubscriptions(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	const tebexGrace = 24 * time.Hour
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			count, err := repo.ExpireSubscriptions(runCtx, now, tebexGrace)
			cancel()
			if err != nil {
				log.Error("failed to expire subscriptions", zap.Error(err))
			} else if count > 0 {
				log.Info("expired subscriptions", zap.Int("count", count))
			}
		}
	}
}

// parseIDs splits a comma-separated list of Twitch ids, dropping blanks and
// non-numeric entries (defensive against a malformed ADMIN_BOOTSTRAP_IDS).
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
