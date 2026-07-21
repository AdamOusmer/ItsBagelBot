package main

import (
	"context"
	"encoding/json"

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
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const serviceName = "modules"

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	client := ent.NewClient(ent.Driver(svcboot.MustEntDriver(log, "bagel_modules")))
	defer func() { _ = client.Close() }()

	svcboot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core, serviceName, "modules-rpc")
	defer func() { _ = n.Pub.Close() }()

	repo := repository.NewModules(client, n.Pub, core.NR, log)
	defer repo.Close(context.Background()) // flushes pending writes on shutdown
	defer closeIntake()                    // stops intake before the repo flush above

	quotes := repository.NewQuotes(client, log)

	consumeEvents(core.Ctx, eventsWiring{
		app: core.NR, broadcast: n.Broadcast, grouped: n.Grouped,
		repo: repo, quotes: quotes, log: log,
	})

	projectionSubject := subscribeRPCs(rpcWiring{
		nc: n.RPC, client: client, repo: repo, quotes: quotes, app: core.NR, log: log,
	})
	health.Serve(env.Get("LISTEN_ADDR", ":8080"), n.RPC.IsConnected)

	log.Info("modules service ready", zap.String("projection_subject", projectionSubject))

	<-core.Ctx.Done()

	log.Info("modules service shutting down")
}

// eventsWiring bundles what consumeEvents needs so the wiring reads as one
// value instead of a long parameter list.
type eventsWiring struct {
	app       *newrelic.Application
	broadcast bus.Subscriber
	grouped   bus.Subscriber
	repo      *repository.Modules
	quotes    *repository.Quotes
	log       *zap.Logger
}

// consumeEvents attaches the service's event subscriptions: cache invalidation
// on the broadcast subscriber, reprojection and account deletion on the
// durable group. Fatal on any subscribe failure, matching main's boot style.
func consumeEvents(ctx context.Context, w eventsWiring) {
	if err := bus.Consume(ctx, w.app, w.broadcast, data.SubjectModuleChanged, func(msg *bus.Message) error {

		var dto data.ModuleChangedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}

		w.repo.Invalidate(dto.UserID)
		return nil
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to module changes", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectReprojectRequest, func(*bus.Message) error {
		return w.repo.Reproject(ctx)
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to reproject requests", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectUserDeleted, func(msg *bus.Message) error {
		return deleteUser(msg, w)
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to user deleted events", zap.Error(err))
	}
}

// deleteUser handles one user_deleted event: validate the payload, then sweep
// the account's module rows and quote book. Malformed payloads are logged and
// dropped (returning an error would only redeliver them).
func deleteUser(msg *bus.Message, w eventsWiring) error {
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
