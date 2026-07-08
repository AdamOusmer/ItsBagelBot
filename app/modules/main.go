package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"github.com/ThreeDotsLabs/watermill/message"

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

	if env.GetBool("DB_AUTO_MIGRATE", true) {
		if err := client.Schema.Create(ctx); err != nil {
			log.Fatal("failed to run migrations", zap.Error(err))
		}
	}

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	rpcURL := bus.RPCURL(natsURL)

	if err := bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	repo := repository.NewModules(client, pub, nrApp, log)
	defer repo.Close(context.Background()) // flushes pending writes on shutdown

	// Govee API-key custody is optional: with no keyset provisioned the store
	// stays nil, its RPCs go unsubscribed, and the govee feature degrades to
	// "not set up" instead of crash-looping this core service. Once
	// TINK_KEYSET_PATH is mounted, keys seal with the modules service's own
	// keyset (distinct from the users service's).
	goveeCreds := loadGoveeCreds(client, log)

	nc, err := bus.Connect(rpcURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	// Broadcast subscription: every instance must drop its cached view when
	// any instance changes a module, so no queue group here.
	broadcast, err := bus.NewSubscriber(natsURL, "", log)
	if err != nil {
		log.Fatal("failed to connect broadcast subscriber", zap.Error(err))
	}
	defer func() { _ = broadcast.Close() }()

	if err := bus.Consume(ctx, nrApp, broadcast, data.SubjectModuleChanged, func(msg *message.Message) error {

		var dto data.ModuleChangedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}

		repo.Invalidate(dto.UserID)
		return nil
	}, log); err != nil {
		log.Fatal("failed to subscribe to module changes", zap.Error(err))
	}

	// Durable group subscription: exactly one instance answers a reproject
	// request by replaying the table as change events.
	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect group subscriber", zap.Error(err))
	}
	defer func() { _ = grouped.Close() }()

	if err := bus.Consume(ctx, nrApp, grouped, data.SubjectReprojectRequest, func(*message.Message) error {
		return repo.Reproject(ctx)
	}, log); err != nil {
		log.Fatal("failed to subscribe to reproject requests", zap.Error(err))
	}

	if err := bus.Consume(ctx, nrApp, grouped, data.SubjectUserDeleted, func(msg *message.Message) error {

		var dto data.UserDeletedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("modules: bad user_deleted payload", zap.Error(err))
			return nil
		}

		if err := validate.UserID(dto.UserID); err != nil {
			log.Warn("modules: invalid user_id in user_deleted", zap.Error(err))
			return nil
		}

		if err := repo.DeleteAllForUser(msg.Context(), dto.UserID); err != nil {
			return err
		}

		// The govee key lives outside the module rows, so it must be swept
		// separately; a missing key is a no-op.
		if goveeCreds != nil {
			if err := goveeCreds.ClearKey(msg.Context(), dto.UserID); err != nil {
				log.Warn("modules: failed to clear govee key on user delete", zap.Uint64("user_id", dto.UserID), zap.Error(err))
			}
		}

		log.Info("modules: deleted all for user", zap.Uint64("user_id", dto.UserID))
		return nil
	}, log); err != nil {
		log.Fatal("failed to subscribe to user deleted events", zap.Error(err))
	}

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get")
	if err := rpc.SubscribeProjection(nc, repo, projectionSubject, "modules-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	// Dashboard verbs (list, upsert): the console toggles/configures modules the
	// same way it manages commands.
	dashboardSubject := env.Get("NATS_MODULES_SUBJECT_PREFIX", "bagel.rpc.modules")
	if err := rpc.SubscribeDashboard(nc, repo, dashboardSubject, "modules-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}

	// Govee key custody: dashboard set/clear/status plus the gateway-only
	// internal decrypt verb. Only wired when a keyset is on hand.
	if goveeCreds != nil {
		goveeDashPrefix := env.Get("NATS_MODULES_GOVEE_SUBJECT_PREFIX", "bagel.rpc.modules.govee")
		goveeInternalPrefix := env.Get("NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key")
		if err := rpc.SubscribeGovee(nc, goveeCreds, goveeDashPrefix, goveeInternalPrefix, "modules-rpc", nrApp, log); err != nil {
			log.Fatal("failed to subscribe govee rpc", zap.Error(err))
		}
		log.Info("govee key custody enabled", zap.String("dashboard_prefix", goveeDashPrefix))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("modules service ready", zap.String("projection_subject", projectionSubject))

	<-ctx.Done()

	log.Info("modules service shutting down")
}

// loadGoveeCreds builds the Govee key custody store from the service's Tink
// keyset. It is best-effort so a core service never crash-loops on a secret
// that may not be provisioned yet: an unset TINK_KEYSET_PATH, or a path whose
// file is not present (the manifest mounts the keyset as an optional secret),
// disables the feature and returns nil. Only a present-but-invalid keyset is
// fatal, since that is a real misconfiguration rather than "feature off".
func loadGoveeCreds(client *ent.Client, log *zap.Logger) *repository.GoveeCreds {
	path := env.Get("TINK_KEYSET_PATH", "")
	if path == "" {
		log.Warn("govee key custody disabled: TINK_KEYSET_PATH not set")
		return nil
	}
	keysetJSON, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn("govee key custody disabled: keyset not provisioned yet", zap.String("path", path))
		return nil
	}
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.Error(err))
	}
	packer, err := crypto.NewCrypto(keysetJSON)
	if err != nil {
		log.Fatal("failed to initialize crypto", zap.Error(err))
	}
	return repository.NewGoveeCreds(client, packer)
}
