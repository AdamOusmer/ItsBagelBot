package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"github.com/ThreeDotsLabs/watermill/message"
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
	nc, pub := connectBus(ctx, natsURL, log)
	defer nc.Close()
	defer func() { _ = pub.Close() }()

	repo := repository.NewUsers(client, packer, pub)
	defer repo.Close()

	closeConsumers := startConsumers(ctx, natsURL, repo, log)
	defer closeConsumers()

	go expireSubscriptions(ctx, repo, log)

	wiring := rpc.Wiring{NC: nc, Repo: repo, App: nrApp, Queue: "users-rpc", Log: log}
	subjects := subscribeRPCs(ctx, wiring, client, log)
	fatalIf(log, bus.SubscribeRPCHealth(nc, serviceName, "users-rpc"), "failed to subscribe rpc health")

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)
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

// connectBus provisions the JetStream streams, opens the RPC connection, and
// builds the bus publisher.
func connectBus(ctx context.Context, natsURL string, log *zap.Logger) (*nats.Conn, bus.Publisher) {
	fatalIf(log, bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log), "failed to provision jetstream streams")

	nc, err := bus.Connect(bus.RPCURL(natsURL), serviceName)
	fatalIf(log, err, "failed to connect to nats")

	pub, err := bus.NewPublisher(natsURL, log)
	fatalIf(log, err, "failed to connect publisher")

	return nc, pub
}

// startConsumers wires the two event-plane consumers: a groupless broadcast
// subscriber that drops each instance's cached view on any user change, and a
// durable-group subscriber where exactly one instance answers a reproject by
// replaying the table. The returned cleanup closes both subscribers.
func startConsumers(ctx context.Context, natsURL string, repo *repository.Users, log *zap.Logger) func() {
	broadcast, err := bus.NewSubscriber(natsURL, "", log)
	fatalIf(log, err, "failed to connect broadcast subscriber")
	fatalIf(log, bus.Consume(ctx, nil, broadcast, data.SubjectUserChanged, invalidateOnUserChange(repo), log),
		"failed to subscribe to user changes")

	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	fatalIf(log, err, "failed to connect group subscriber")
	fatalIf(log, bus.Consume(ctx, nil, grouped, data.SubjectReprojectRequest, func(*message.Message) error {
		return repo.Reproject(ctx)
	}, log), "failed to subscribe to reproject requests")

	return func() {
		_ = grouped.Close()
		_ = broadcast.Close()
	}
}

// invalidateOnUserChange drops the local cached view for a changed user.
func invalidateOnUserChange(repo *repository.Users) func(*message.Message) error {
	return func(msg *message.Message) error {
		var dto data.UserChangedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
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
func subscribeRPCs(ctx context.Context, wiring rpc.Wiring, client *ent.Client, log *zap.Logger) rpcSubjects {
	invalidationPrefix := env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate")

	s := rpcSubjects{
		dashboard:  env.Get("NATS_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.dashboard"),
		admin:      env.Get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user"),
		billing:    env.Get("NATS_INTERNAL_BILLING_SUBJECT", "bagel.rpc.internal.billing.apply"),
		projection: env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
	}

	fatalIf(log, rpc.SubscribeDashboard(wiring, s.dashboard, invalidationPrefix), "failed to subscribe dashboard rpc")
	fatalIf(log, rpc.SubscribeAdmin(wiring, s.admin, invalidationPrefix), "failed to subscribe admin rpc")
	fatalIf(log, rpc.SubscribeBilling(wiring, s.billing, invalidationPrefix), "failed to subscribe billing rpc")

	// Admin authorization + audit. Seed the bootstrap owners/admins so a fresh
	// DB is never locked out, then serve the auth.check / auth.* / audit.*
	// surface the console uses in place of the old static env allowlist.
	seedBootstrapStaff(ctx, client, log)
	authPrefix := env.Get("NATS_ADMIN_AUTH_SUBJECT_PREFIX", "bagel.rpc.admin.user.auth")
	auditPrefix := env.Get("NATS_ADMIN_AUDIT_SUBJECT_PREFIX", "bagel.rpc.admin.user.audit")
	fatalIf(log, rpc.SubscribeAdminAuth(wiring, client, authPrefix, auditPrefix), "failed to subscribe admin auth rpc")

	fatalIf(log, rpc.SubscribeProjection(wiring, s.projection), "failed to subscribe projection rpc")
	fatalIf(log, rpc.SubscribeEmail(wiring, env.Get("NATS_INTERNAL_USERS_EMAIL_SUBJECT", "bagel.rpc.internal.users.email.get")),
		"failed to subscribe email rpc")
	fatalIf(log, rpc.SubscribeTokens(wiring, env.Get("NATS_INTERNAL_TOKENS_SUBJECT_PREFIX", "bagel.rpc.internal.tokens")),
		"failed to subscribe tokens rpc")
	fatalIf(log, rpc.SubscribeDelegation(wiring, env.Get("NATS_DELEGATION_SUBJECT_PREFIX", "bagel.rpc.delegation"), invalidationPrefix),
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
