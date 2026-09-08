// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"

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

	if err := bus.ServeVerbs(w.RPCWiring, prefix,
		bus.At("list", d.handleList),
		bus.At("upsert", d.handleUpsert),
		bus.At("patch", d.handlePatch),
	); err != nil {
		return err
	}

	// The Govee key-custody verbs (set/clear/status for the dashboard, plus the
	// gossip-only internal decrypt) are part of the modules service's RPC
	// surface, wired here so main keeps a single subscribe call. A no-op when
	// key custody is disabled (no keyset). wireSpotify rides the same split
	// for the connected-account refresh tokens.
	if err := wireGovee(goveeWiring{nc: w.NC, creds: w.Repo.Govee(), queueGroup: w.Queue, app: w.App, log: w.Log}); err != nil {
		return err
	}
	return wireSpotify(spotifyWiring{nc: w.NC, creds: w.Repo.Spotify(), queueGroup: w.Queue, app: w.App, log: w.Log})
}

func (d *dashboardRPC) parseUserID(req modulesrpc.DashboardRequest) (uint64, bool, modulesrpc.DashboardReply) {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, false, modulesrpc.DashboardReply{Error: "invalid user_id"}
	}
	return id, true, modulesrpc.DashboardReply{}
}

func (d *dashboardRPC) handleList(ctx context.Context, req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return modulesrpc.DashboardReply{Error: err.Error()}
	}
	return modulesrpc.DashboardReply{Modules: views}
}

func (d *dashboardRPC) handleUpsert(ctx context.Context, req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	if err := d.repo.Set(id, req.Name, req.IsEnabled, req.Configs); err != nil {
		return modulesrpc.DashboardReply{Error: err.Error()}
	}

	return modulesrpc.DashboardReply{}
}

// handlePatch merges a subset of config keys into a module under optimistic
// concurrency: Configs carries only the keys to change, and ExpectedRev (when
// set) must match the stored revision or the write is reported as a conflict for
// the client to refetch and retry.
func (d *dashboardRPC) handlePatch(ctx context.Context, req modulesrpc.DashboardRequest) modulesrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	partial := map[string]codec.RawMessage{}
	if len(req.Configs) > 0 {
		if err := codec.Unmarshal(req.Configs, &partial); err != nil {
			return modulesrpc.DashboardReply{Error: "invalid configs"}
		}
	}

	res, err := d.repo.Patch(ctx, id, req.Name, req.IsEnabled, partial, req.ExpectedRev)
	if err != nil {
		return modulesrpc.DashboardReply{Error: err.Error()}
	}
	return modulesrpc.DashboardReply{Rev: res.Rev, Conflict: res.Conflict}
}
