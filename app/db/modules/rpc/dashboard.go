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

func (d *dashboardRPC) handleUpsert(_ context.Context, req modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	return modulesrpc.DashboardReply{}, d.repo.Set(id, req.Name, req.IsEnabled, req.Configs)
}

func (d *dashboardRPC) handlePatch(ctx context.Context, req modulesrpc.DashboardRequest, id uint64) (modulesrpc.DashboardReply, error) {
	partial := map[string]codec.RawMessage{}
	if len(req.Configs) > 0 {
		if err := codec.Unmarshal(req.Configs, &partial); err != nil {
			return modulesrpc.DashboardReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid configs")}, nil
		}
	}

	res, err := d.repo.Patch(ctx, id, req.Name, req.IsEnabled, partial, req.ExpectedRev)
	if err != nil {
		return modulesrpc.DashboardReply{}, err
	}
	return modulesrpc.DashboardReply{Rev: res.Rev, Conflict: res.Conflict}, nil
}
