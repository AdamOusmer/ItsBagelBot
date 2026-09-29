// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/commands/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	commandsrpc "ItsBagelBot/internal/domain/rpc/commands"
	"ItsBagelBot/pkg/bus"
)

type dashboardRPC struct {
	repo *repository.Commands
	log  *zap.Logger
}

func SubscribeDashboard(w Wiring, prefix string) error {
	d := &dashboardRPC{repo: w.Commands, log: w.Log}

	return bus.ServeVerbs(w.RPCWiring, prefix,
		bus.VerbForUser[commandsrpc.DashboardRequest, commandsrpc.DashboardReply]("list", d.handleList),
		bus.VerbForUser[commandsrpc.DashboardRequest, commandsrpc.DashboardReply]("upsert", d.handleUpsert),
		bus.VerbForUser[commandsrpc.DashboardRequest, commandsrpc.DashboardReply]("delete", d.handleDelete),
	)
}

func (d *dashboardRPC) handleList(ctx context.Context, _ commandsrpc.DashboardRequest, id uint64) (commandsrpc.DashboardReply, error) {
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return commandsrpc.DashboardReply{}, err
	}
	return commandsrpc.DashboardReply{Commands: views}, nil
}

func (d *dashboardRPC) handleUpsert(ctx context.Context, req commandsrpc.DashboardRequest, id uint64) (commandsrpc.DashboardReply, error) {
	spec, ok := specFromRequest(req)
	if !ok {
		return commandsrpc.DashboardReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid allowed_user_id")}, nil
	}
	if isRename(req) {
		return commandsrpc.DashboardReply{}, d.repo.Rename(ctx, id, req.OriginalName, spec)
	}
	if reply, done, err := d.tryRestore(ctx, req, id, spec); done {
		return reply, err
	}
	return commandsrpc.DashboardReply{}, d.repo.Upsert(id, spec)
}

func isRename(req commandsrpc.DashboardRequest) bool {
	return req.OriginalName != "" && req.OriginalName != req.Name
}

func (d *dashboardRPC) tryRestore(ctx context.Context, req commandsrpc.DashboardRequest, id uint64, spec repository.CommandSpec) (commandsrpc.DashboardReply, bool, error) {
	if req.RestoreUses <= 0 || req.OriginalName != "" {
		return commandsrpc.DashboardReply{}, false, nil
	}
	restored, err := d.repo.Restore(ctx, id, spec, req.RestoreUses)
	return commandsrpc.DashboardReply{Restored: restored}, err != nil || restored, err
}

func specFromRequest(req commandsrpc.DashboardRequest) (repository.CommandSpec, bool) {
	allowedUserID, ok := parseAllowedUserID(req.AllowedUserID)
	return repository.CommandSpec{
		Name:             req.Name,
		Aliases:          req.Aliases,
		Response:         req.Response,
		IsActive:         req.IsActive,
		StreamOnlineOnly: req.StreamOnlineOnly,
		Perm:             req.Perm,
		Cooldown:         req.Cooldown,
		AllowedUserID:    allowedUserID,
		BumpCounter:      req.BumpCounter,
	}, ok
}

func parseAllowedUserID(raw string) (uint64, bool) {
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	return parsed, err == nil
}

func (d *dashboardRPC) handleDelete(ctx context.Context, req commandsrpc.DashboardRequest, id uint64) (commandsrpc.DashboardReply, error) {
	return commandsrpc.DashboardReply{}, d.repo.Delete(ctx, id, req.Name)
}
