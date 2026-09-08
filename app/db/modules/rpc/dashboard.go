// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

type dashboardRPC struct {
	repo *repository.Modules
	log  *zap.Logger
}

// SubscribeDashboard wires the modules dashboard verbs (list, upsert, patch)
// under prefix, mirroring the commands service so the console manages modules
// the same way it manages commands.
func SubscribeDashboard(w Wiring, prefix string) error {
	d := &dashboardRPC{repo: w.Repo, log: w.Log}

	// VerbForUser, not At: the user-id guard is the whole prologue of all three
	// handlers, so it is bound once here rather than re-derived in each.
	if err := bus.ServeVerbs(w.RPCWiring, prefix,
		bus.VerbForUser[modulesrpc.DashboardRequest, modulesrpc.DashboardReply]("list", d.handleList),
		bus.VerbForUser[modulesrpc.DashboardRequest, modulesrpc.DashboardReply]("upsert", d.handleUpsert),
		bus.VerbForUser[modulesrpc.DashboardRequest, modulesrpc.DashboardReply]("patch", d.handlePatch),
	); err != nil {
		return err
	}

	// The Govee key-custody verbs (set/clear/status for the dashboard, plus the
	// gossip-only internal decrypt) are part of the modules service's RPC
	// surface, wired here so main keeps a single subscribe call. A no-op when
	// key custody is disabled (no keyset). wireSpotify rides the same split
	// for the connected-account refresh tokens.
	if err := wireGovee(w.RPCWiring, w.Repo.Govee()); err != nil {
		return err
	}
	return wireSpotify(w.RPCWiring, w.Repo.Spotify())
}

func (d *dashboardRPC) handleList(ctx context.Context, _ modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return modulesrpc.DashboardReply{}, err
	}
	return modulesrpc.DashboardReply{Modules: views}, nil
}

func (d *dashboardRPC) handleUpsert(_ context.Context, req modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	return modulesrpc.DashboardReply{}, d.repo.Set(id, req.Name, req.IsEnabled, req.Configs)
}

// handlePatch merges a subset of config keys into a module under optimistic
// concurrency: Configs carries only the keys to change, and ExpectedRev (when
// set) must match the stored revision or the write is reported as a conflict for
// the client to refetch and retry.
func (d *dashboardRPC) handlePatch(ctx context.Context, req modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	partial := map[string]codec.RawMessage{}
	if len(req.Configs) > 0 {
		if err := codec.Unmarshal(req.Configs, &partial); err != nil {
			return modulesrpc.DashboardReply{Error: "invalid configs"}, nil
		}
	}

	res, err := d.repo.Patch(ctx, id, req.Name, req.IsEnabled, partial, req.ExpectedRev)
	if err != nil {
		return modulesrpc.DashboardReply{}, err
	}
	return modulesrpc.DashboardReply{Rev: res.Rev, Conflict: res.Conflict}, nil
}
