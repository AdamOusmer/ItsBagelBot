package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/modules/repository"
	goveerpc "ItsBagelBot/internal/domain/rpc/govee"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
)

// goveeWiring bundles what wireGovee needs beyond the subject prefixes (which it
// reads from the environment itself): the RPC connection, the credential store,
// the shared queue group, and the New Relic app + logger.
type goveeWiring struct {
	nc         *nats.Conn
	creds      *repository.GoveeCreds
	queueGroup string
	app        *newrelic.Application
	log        *zap.Logger
}

// wireGovee subscribes the Govee API-key custody RPCs. The dashboard verbs
// (set/clear/status) never echo the key; the internal decrypt verb is
// account-scoped to the gateway, the one service that dials Govee — the same
// split the users service uses for tokens and contact email. It is a no-op when
// key custody is disabled (nil store).
func wireGovee(w goveeWiring) error {
	if w.creds == nil {
		return nil
	}
	dash := env.Get("NATS_MODULES_GOVEE_SUBJECT_PREFIX", "bagel.rpc.modules.govee")
	internal := env.Get("NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key")
	g := &goveeRPC{creds: w.creds, log: w.log}

	if err := bus.QueueSubscribeJSON[goveerpc.KeySetRequest, goveerpc.KeyMutateReply](
		w.nc, dash+".set", w.queueGroup, 3*time.Second, w.app, w.log, g.handleSet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[goveerpc.KeyClearRequest, goveerpc.KeyMutateReply](
		w.nc, dash+".clear", w.queueGroup, 3*time.Second, w.app, w.log, g.handleClear); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[goveerpc.KeyStatusRequest, goveerpc.KeyStatusReply](
		w.nc, dash+".status", w.queueGroup, 3*time.Second, w.app, w.log, g.handleStatus); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[goveerpc.KeyGetRequest, goveerpc.KeyGetReply](
		w.nc, internal+".get", w.queueGroup, 3*time.Second, w.app, w.log, g.handleGet); err != nil {
		return err
	}
	w.log.Info("govee key custody enabled", zap.String("dashboard_prefix", dash))
	return nil
}

type goveeRPC struct {
	creds *repository.GoveeCreds
	log   *zap.Logger
}

func (g *goveeRPC) handleSet(ctx context.Context, req goveerpc.KeySetRequest) goveerpc.KeyMutateReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return goveerpc.KeyMutateReply{Error: "user_id must be numeric"}
	}
	if err := g.creds.SetKey(ctx, id, req.Key); err != nil {
		// The error never carries the key; it is a validation or seal failure.
		return goveerpc.KeyMutateReply{Error: err.Error()}
	}
	return goveerpc.KeyMutateReply{}
}

func (g *goveeRPC) handleClear(ctx context.Context, req goveerpc.KeyClearRequest) goveerpc.KeyMutateReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return goveerpc.KeyMutateReply{Error: "user_id must be numeric"}
	}
	if err := g.creds.ClearKey(ctx, id); err != nil {
		return goveerpc.KeyMutateReply{Error: err.Error()}
	}
	return goveerpc.KeyMutateReply{}
}

func (g *goveeRPC) handleStatus(ctx context.Context, req goveerpc.KeyStatusRequest) goveerpc.KeyStatusReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return goveerpc.KeyStatusReply{Error: "user_id must be numeric"}
	}
	present, err := g.creds.HasKey(ctx, id)
	if err != nil {
		return goveerpc.KeyStatusReply{Error: err.Error()}
	}
	return goveerpc.KeyStatusReply{Present: present}
}

func (g *goveeRPC) handleGet(ctx context.Context, req goveerpc.KeyGetRequest) goveerpc.KeyGetReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return goveerpc.KeyGetReply{Error: "user_id must be numeric"}
	}
	key, err := g.creds.Key(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNoGoveeKey):
		return goveerpc.KeyGetReply{}
	case err != nil:
		return goveerpc.KeyGetReply{Error: err.Error()}
	}
	return goveerpc.KeyGetReply{Key: key}
}
