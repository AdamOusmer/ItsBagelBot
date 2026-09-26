// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/codec"

	"ItsBagelBot/app/db/modules/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/modules/ent/runtime"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/db/modules/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	serviceName = "modules"
	queueGroup  = "modules-rpc"
)

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, "bagel_modules")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()

	repo := repository.NewModules(client, n.Pub, core.NR, log)
	repo.SetAccountInstanceResolver(func(ctx context.Context, id uint64) (int64, error) {
		requested := strconv.FormatUint(id, 10)
		body, err := codec.Marshal(projection.Request{UserID: requested})
		if err != nil {
			return 0, err
		}
		msg, err := bus.RequestWithContext(ctx, n.RPC, env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"), body)
		if err != nil {
			return 0, err
		}
		var reply projection.UserReply
		if err := codec.Unmarshal(msg.Data, &reply); err != nil {
			return 0, err
		}
		return canonicalAccountInstance(reply, requested)
	})
	defer repo.Close(context.Background())
	defer closeIntake() // stops intake before the repo flush above

	quotes := repository.NewQuotes(client, log)

	consumeEvents(core.Ctx, eventsWiring{
		app: core.NR, broadcast: n.Broadcast, grouped: n.Grouped,
		repo: repo, quotes: quotes, log: log,
	})

	projectionSubject := subscribeRPCs(rpcWiring{
		nc: n.RPC, client: client, repo: repo, quotes: quotes, app: core.NR, log: log,
	})
	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	}, bus.LaneCheck("data", n.Grouped))

	log.Info("modules service ready", zap.String("projection_subject", projectionSubject))

	core.Await()
}

type eventsWiring struct {
	app       *newrelic.Application
	broadcast bus.Subscriber
	grouped   bus.Subscriber
	repo      *repository.Modules
	quotes    *repository.Quotes
	log       *zap.Logger
}

func consumeEvents(ctx context.Context, w eventsWiring) {
	invalidate := consumers.OnChangeInvalidate(changedUserID, w.repo.Invalidate)
	if err := bus.Consume(ctx, w.app, w.broadcast, data.SubjectModuleChanged, invalidate, w.log); err != nil {
		w.log.Fatal("failed to subscribe to module changes", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectReprojectRequest, func(*bus.Message) error {
		return w.repo.Reproject(ctx)
	}, w.log); err != nil {
		w.log.Fatal("failed to subscribe to reproject requests", zap.Error(err))
	}

	if err := bus.Consume(ctx, w.app, w.grouped, data.SubjectUserDeleted, deleteUser(w), w.log); err != nil {
		w.log.Fatal("failed to subscribe to user deleted events", zap.Error(err))
	}
}

func changedUserID(dto data.ModuleChangedDTO) uint64 { return dto.UserID }

func deleteUser(w eventsWiring) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.UserDeletedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			return nil
		}
		if err := validate.UserID(dto.UserID); err != nil {
			return nil
		}
		ctx := msg.Context()
		userID := dto.UserID
		return w.repo.DeleteAccount(ctx, userID, dto.AccountCreatedAt)
	}
}

type rpcWiring struct {
	nc     *nats.Conn
	client *ent.Client
	repo   *repository.Modules
	quotes *repository.Quotes
	app    *newrelic.Application
	log    *zap.Logger
}

func subscribeRPCs(w rpcWiring) string {
	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: w.nc, App: w.app, Queue: queueGroup, Log: w.log},
		Repo:      w.repo,
	}

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get")
	if err := rpc.SubscribeProjection(wiring, projectionSubject); err != nil {
		w.log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	dashboardSubject := env.Get("NATS_MODULES_SUBJECT_PREFIX", "bagel.rpc.modules")
	if err := rpc.SubscribeDashboard(wiring, dashboardSubject); err != nil {
		w.log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}

	if err := rpc.SubscribeQuotes(wiring.RPCWiring, w.quotes, dashboardSubject+".quote"); err != nil {
		w.log.Fatal("failed to subscribe quotes rpc", zap.Error(err))
	}

	personality := repository.NewPersonality(w.client)
	if err := rpc.SubscribePersonality(wiring.RPCWiring, personality, dashboardSubject+".personality"); err != nil {
		w.log.Fatal("failed to subscribe personality rpc", zap.Error(err))
	}
	return projectionSubject
}

// Only the canonical service's explicit not-found reply proves absence. Other
// refusals or unstamped successes cannot authorize destructive account cleanup.
func canonicalAccountInstance(reply projection.UserReply, requested string) (int64, error) {
	if reply.Code == domainrpc.CodeNotFound {
		if reply.UserID != requested || reply.AccountCreatedAt != 0 {
			return 0, errors.New("invalid canonical absence identity")
		}
		return 0, nil
	}
	if reply.Error != "" {
		return 0, errors.New(reply.Error)
	}
	if reply.Code != domainrpc.CodeOK {
		return 0, fmt.Errorf("canonical account refusal: %s", reply.Code)
	}
	if reply.UserID != requested || reply.AccountCreatedAt <= 0 {
		return 0, errors.New("invalid canonical account identity")
	}
	return reply.AccountCreatedAt, nil
}
