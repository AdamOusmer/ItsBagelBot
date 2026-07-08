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
)

// SubscribeGovee wires the Govee API-key custody RPCs. The dashboard verbs
// (set/clear/status) run under dashPrefix and never echo the key; the internal
// decrypt verb runs at internalPrefix+".get" and is account-scoped to the
// gateway, the one service that dials Govee — the same split the users service
// uses for tokens and contact email.
func SubscribeGovee(nc *nats.Conn, creds *repository.GoveeCreds, dashPrefix, internalPrefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	g := &goveeRPC{creds: creds, log: log}

	if err := bus.QueueSubscribeJSON[goveerpc.KeySetRequest, goveerpc.KeyMutateReply](
		nc, dashPrefix+".set", queueGroup, 3*time.Second, app, log, g.handleSet); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[goveerpc.KeyClearRequest, goveerpc.KeyMutateReply](
		nc, dashPrefix+".clear", queueGroup, 3*time.Second, app, log, g.handleClear); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[goveerpc.KeyStatusRequest, goveerpc.KeyStatusReply](
		nc, dashPrefix+".status", queueGroup, 3*time.Second, app, log, g.handleStatus); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[goveerpc.KeyGetRequest, goveerpc.KeyGetReply](
		nc, internalPrefix+".get", queueGroup, 3*time.Second, app, log, g.handleGet)
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
