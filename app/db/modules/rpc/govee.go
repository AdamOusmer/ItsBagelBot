// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/modules/repository"
	goveerpc "ItsBagelBot/internal/domain/rpc/govee"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
)

// wireGovee subscribes the Govee API-key custody RPCs. The dashboard verbs
// (set/clear/status) never echo the key; the internal decrypt verb is
// account-scoped to gossip, the one service that dials Govee — the same
// split the users service uses for tokens and contact email. It is a no-op when
// key custody is disabled (nil store).
//
// The four verbs carry different request and reply types, so they cannot share
// one ServeVerbs table; each is bound on its own through the same wiring.
func wireGovee(w bus.RPCWiring, creds *repository.GoveeCreds) error {
	if creds == nil {
		return nil
	}
	dash := env.Get("NATS_MODULES_GOVEE_SUBJECT_PREFIX", "bagel.rpc.modules.govee")
	internal := env.Get("NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key")
	g := &goveeRPC{creds: creds, log: w.Log}

	custody := w.Within(custodyBudget)
	if err := bus.Serve(custody, dash+".set", g.handleSet); err != nil {
		return err
	}
	if err := bus.Serve(custody, dash+".clear", g.handleClear); err != nil {
		return err
	}
	if err := bus.Serve(custody, dash+".status", g.handleStatus); err != nil {
		return err
	}
	if err := bus.Serve(custody, internal+".get", g.handleGet); err != nil {
		return err
	}
	w.Log.Info("govee key custody enabled", zap.String("dashboard_prefix", dash))
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
