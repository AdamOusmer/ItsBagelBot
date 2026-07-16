package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/modules/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/modules/ent/runtime"
	"ItsBagelBot/app/modules/repository"
	"ItsBagelBot/app/modules/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const serviceName = "modules"

func main() {
	validate.CheckFloor = moderation.CheckFloor

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
		Schema:   env.Get("DB_SCHEMA", "bagel_modules"),
	})
	if err != nil {
		log.Fatal("failed to open database", zap.Error(err))
	}

	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	migrateSchema(ctx, client, log)

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	rpcURL := bus.RPCURL(natsURL)

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	repo := repository.NewModules(client, pub, nrApp, log)
	defer repo.Close(context.Background()) // flushes pending writes on shutdown

	quotes := repository.NewQuotes(client, log)

	nc := connectRPC(rpcURL, log)
	defer nc.Close()

	// Broadcast subscription: every instance must drop its cached view when
	// any instance changes a module, so no queue group here.
	broadcast, err := bus.NewSubscriber(natsURL, "", log)
	if err != nil {
		log.Fatal("failed to connect broadcast subscriber", zap.Error(err))
	}
	defer func() { _ = broadcast.Close() }()

	// Durable group subscription: exactly one instance answers a reproject
	// request by replaying the table as change events.
	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect group subscriber", zap.Error(err))
	}
	defer func() { _ = grouped.Close() }()

	consumeEvents(ctx, eventsWiring{
		app: nrApp, broadcast: broadcast, grouped: grouped,
		repo: repo, quotes: quotes, log: log,
	})

	projectionSubject := subscribeRPCs(rpcWiring{
		nc: nc, client: client, repo: repo, quotes: quotes, app: nrApp, log: log,
	})
	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("modules service ready", zap.String("projection_subject", projectionSubject))

	<-ctx.Done()

	log.Info("modules service shutting down")
}

// migrateSchema runs the ent auto-migration unless disabled. Fatal on failure:
// a modules instance without its tables can only crashloop later anyway.
func migrateSchema(ctx context.Context, client *ent.Client, log *zap.Logger) {
	if !env.GetBool("DB_AUTO_MIGRATE", true) {
		return
	}
	if err := client.Schema.Create(ctx); err != nil {
		log.Fatal("failed to run migrations", zap.Error(err))
	}
}

// eventsWiring bundles what consumeEvents needs so the wiring reads as one
// value instead of a long parameter list.
type eventsWiring struct {
	app       *newrelic.Application
	broadcast message.Subscriber
	grouped   message.Subscriber
	repo      *repository.Modules
	quotes    *repository.Quotes
	log       *zap.Logger
}

// consumeEvents attaches the service's event subscriptions: cache invalidation
// on the broadcast subscriber, reprojection and account deletion on the
// durable group. Fatal on any subscribe failure, matching main's boot style.
func consumeEvents(ctx context.Context, w eventsWiring) {
	if err := bus.Consume(ctx, w.app, w.broadcast, data.SubjectModuleChanged, func(msg *message.Message) error {

		var dto data.ModuleChangedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}

		w.repo.Invalidate(dto.UserID)
		return nil
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to module changes", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectReprojectRequest, func(*message.Message) error {
		return w.repo.Reproject(ctx)
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to reproject requests", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectUserDeleted, func(msg *message.Message) error {
		return deleteUser(msg, w)
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to user deleted events", zap.Error(err))
	}
}

// deleteUser handles one user_deleted event: validate the payload, then sweep
// the account's module rows and quote book. Malformed payloads are logged and
// dropped (returning an error would only redeliver them).
func deleteUser(msg *message.Message, w eventsWiring) error {
	var dto data.UserDeletedDTO
	if err := json.Unmarshal(msg.Payload, &dto); err != nil {
		w.log.Warn("modules: bad user_deleted payload", zap.Error(err))
		return nil
	}

	if err := validate.UserID(dto.UserID); err != nil {
		w.log.Warn("modules: invalid user_id in user_deleted", zap.Error(err))
		return nil
	}

	if err := w.repo.DeleteAllForUser(msg.Context(), dto.UserID); err != nil {
		return err
	}

	if err := w.quotes.DeleteAllForUser(msg.Context(), dto.UserID); err != nil {
		return err
	}

	w.log.Info("modules: deleted all for user", zap.Uint64("user_id", dto.UserID))
	return nil
}

// rpcWiring bundles what subscribeRPCs needs, mirroring eventsWiring.
type rpcWiring struct {
	nc     *nats.Conn
	client *ent.Client
	repo   *repository.Modules
	quotes *repository.Quotes
	app    *newrelic.Application
	log    *zap.Logger
}

// subscribeRPCs answers the service's request/reply verbs: the internal
// projection read, the dashboard verbs, the channel-quotes verbs and the
// personality verbs. Returns the projection subject for the ready banner.
// Fatal on any subscribe failure, matching main's boot style.
func subscribeRPCs(w rpcWiring) string {
	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get")
	if err := rpc.SubscribeProjection(w.nc, w.repo, projectionSubject, "modules-rpc", w.app, w.log); err != nil {
		w.log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	// Dashboard verbs (list, upsert): the console toggles/configures modules the
	// same way it manages commands.
	dashboardSubject := env.Get("NATS_MODULES_SUBJECT_PREFIX", "bagel.rpc.modules")
	if err := rpc.SubscribeDashboard(w.nc, w.repo, dashboardSubject, "modules-rpc", w.app, w.log); err != nil {
		w.log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}

	// Channel-quotes verbs (the sesame quotes module's store).
	if err := rpc.SubscribeQuotes(rpc.QuotesWiring{
		NC:         w.nc,
		Repo:       w.quotes,
		Prefix:     dashboardSubject + ".quote",
		QueueGroup: "modules-rpc",
		App:        w.app,
		Log:        w.log,
	}); err != nil {
		w.log.Fatal("failed to subscribe quotes rpc", zap.Error(err))
	}

	// Personality verbs (the sesame personality module's permanent feed counter).
	if err := rpc.SubscribePersonality(rpc.PersonalityWiring{
		NC:         w.nc,
		Repo:       repository.NewPersonality(w.client),
		Prefix:     dashboardSubject + ".personality",
		QueueGroup: "modules-rpc",
		App:        w.app,
		Log:        w.log,
	}); err != nil {
		w.log.Fatal("failed to subscribe personality rpc", zap.Error(err))
	}
	return projectionSubject
}

func connectRPC(url string, log *zap.Logger) *nats.Conn {
	nc, err := bus.Connect(url, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	if err := bus.SubscribeRPCHealth(nc, serviceName, "modules-rpc"); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}
	return nc
}
