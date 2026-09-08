// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

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
// one ServeVerbs table; each is bound on its own through the same wiring, and
// ServeForUser puts the fleet-wide user-id guard in front of every one.
func wireGovee(w bus.RPCWiring, creds *repository.GoveeCreds) error {
	if creds == nil {
		return nil
	}
	dash := env.Get("NATS_MODULES_GOVEE_SUBJECT_PREFIX", "bagel.rpc.modules.govee")
	internal := env.Get("NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key")
	g := &goveeRPC{creds: creds, log: w.Log}

	custody := w.Within(custodyBudget)
	if err := errors.Join(
		bus.ServeForUser[goveerpc.KeySetRequest, goveerpc.KeyMutateReply](custody, dash+".set", g.handleSet),
		bus.ServeForUser[goveerpc.KeyClearRequest, goveerpc.KeyMutateReply](custody, dash+".clear", g.handleClear),
		bus.ServeForUser[goveerpc.KeyStatusRequest, goveerpc.KeyStatusReply](custody, dash+".status", g.handleStatus),
		bus.ServeForUser[goveerpc.KeyGetRequest, goveerpc.KeyGetReply](custody, internal+".get", g.handleGet),
	); err != nil {
		return err
	}
	w.Log.Info("govee key custody enabled", zap.String("dashboard_prefix", dash))
	return nil
}

type goveeRPC struct {
	creds *repository.GoveeCreds
	log   *zap.Logger
}

// A surfaced error never carries the key: these writes take their plaintext as
// an argument and fail on validation or sealing.
func (g *goveeRPC) handleSet(ctx context.Context, req goveerpc.KeySetRequest, id uint64) (goveerpc.KeyMutateReply, error) {
	return goveerpc.KeyMutateReply{}, g.creds.SetKey(ctx, id, req.Key)
}

func (g *goveeRPC) handleClear(ctx context.Context, _ goveerpc.KeyClearRequest, id uint64) (goveerpc.KeyMutateReply, error) {
	return goveerpc.KeyMutateReply{}, g.creds.ClearKey(ctx, id)
}

func (g *goveeRPC) handleStatus(ctx context.Context, _ goveerpc.KeyStatusRequest, id uint64) (goveerpc.KeyStatusReply, error) {
	present, err := g.creds.HasKey(ctx, id)
	if err != nil {
		return goveerpc.KeyStatusReply{}, err
	}
	return goveerpc.KeyStatusReply{Present: present}, nil
}

func (g *goveeRPC) handleGet(ctx context.Context, _ goveerpc.KeyGetRequest, id uint64) (goveerpc.KeyGetReply, error) {
	key, err := g.creds.Key(ctx, id)
	if errors.Is(err, repository.ErrNoGoveeKey) {
		// "None on file" is an empty reply, not a failure.
		return goveerpc.KeyGetReply{}, nil
	}
	return goveerpc.KeyGetReply{Key: key}, err
}
