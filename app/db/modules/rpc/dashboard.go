// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/modules/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
)

type dashboardRPC struct {
	repo *repository.Modules
	log  *zap.Logger
}

func SubscribeDashboard(w Wiring, prefix string) error {
	d := &dashboardRPC{repo: w.Repo, log: w.Log}

	if err := bus.ServeVerbs(w.RPCWiring, prefix,
		bus.VerbForUser[modulesrpc.DashboardRequest, modulesrpc.DashboardReply]("list", d.handleList),
		bus.VerbForUser[modulesrpc.DashboardRequest, modulesrpc.DashboardReply]("upsert", d.handleUpsert),
		bus.VerbForUser[modulesrpc.DashboardRequest, modulesrpc.DashboardReply]("patch", d.handlePatch),
		bus.VerbForUser[modulesrpc.DashboardRequest, modulesrpc.DashboardReply]("patch-existing", d.handlePatchExisting),
	); err != nil {
		return err
	}

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

func (d *dashboardRPC) handleUpsert(ctx context.Context, req modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	views, err := d.repo.SetNow(ctx, id, req.Name, req.IsEnabled, req.Configs)
	return modulesrpc.DashboardReply{Modules: views}, err
}

func (d *dashboardRPC) handlePatch(ctx context.Context, req modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	return d.patch(ctx, req, id, false)
}

func (d *dashboardRPC) handlePatchExisting(ctx context.Context, req modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	if !validExistingPatchGuard(req) {
		return modulesrpc.DashboardReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "expected row ID and revision are required")}, nil
	}
	return d.patch(ctx, req, id, true)
}

func validExistingPatchGuard(req modulesrpc.DashboardRequest) bool {
	if req.ExpectedID == nil || req.ExpectedRev == nil {
		return false
	}
	if *req.ExpectedID <= 0 {
		return false
	}
	return *req.ExpectedRev >= 0
}

func (d *dashboardRPC) patch(ctx context.Context, req modulesrpc.DashboardRequest, id uint64, existingOnly bool) (modulesrpc.DashboardReply, error) {
	partial := map[string]codec.RawMessage{}
	if len(req.Configs) > 0 {
		if err := codec.Unmarshal(req.Configs, &partial); err != nil {
			return modulesrpc.DashboardReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid configs")}, nil
		}
	}

	var res repository.PatchResult
	var err error
	if existingOnly {
		res, err = d.repo.PatchExisting(ctx, id, req.Name, req.IsEnabled, partial, *req.ExpectedRev, *req.ExpectedID)
	} else {
		res, err = d.repo.Patch(ctx, id, req.Name, req.IsEnabled, partial, req.ExpectedRev)
	}
	if err != nil {
		return modulesrpc.DashboardReply{}, err
	}
	if res.Conflict {
		return modulesrpc.DashboardReply{Rev: res.Rev, Conflict: true}, nil
	}
	views, err := d.repo.List(ctx, id)
	return modulesrpc.DashboardReply{Rev: res.Rev, Modules: views}, err
}
