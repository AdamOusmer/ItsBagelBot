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

// SubscribeDashboard wires the console's command verbs under prefix.
func SubscribeDashboard(w Wiring, prefix string) error {
	d := &dashboardRPC{repo: w.Commands, log: w.Log}

	// VerbForUser, not At: the user-id guard is the whole prologue of all three
	// handlers, so it is bound once here rather than re-derived in each.
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
	// allowed_user_id is optional; empty/"0" means no per-user restriction. It
	// is not the request's own user, so it keeps its own refusal string.
	var allowedUserID uint64
	if req.AllowedUserID != "" {
		parsed, err := strconv.ParseUint(req.AllowedUserID, 10, 64)
		if err != nil {
			return commandsrpc.DashboardReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid allowed_user_id")}, nil
		}
		allowedUserID = parsed
	}

	spec := repository.CommandSpec{
		Name:             req.Name,
		Aliases:          req.Aliases,
		Response:         req.Response,
		IsActive:         req.IsActive,
		StreamOnlineOnly: req.StreamOnlineOnly,
		Perm:             req.Perm,
		Cooldown:         req.Cooldown,
		AllowedUserID:    allowedUserID,
	}

	// A rename updates the existing row's name field in place; a plain edit or
	// create goes through the write-behind upsert.
	// A validation or conflict error from either write reaches the caller as
	// the reply's error field, which is what the guard does with it.
	if req.OriginalName != "" && req.OriginalName != req.Name {
		return commandsrpc.DashboardReply{}, d.repo.Rename(ctx, id, req.OriginalName, spec)
	}
	return commandsrpc.DashboardReply{}, d.repo.Upsert(id, spec)
}

func (d *dashboardRPC) handleDelete(ctx context.Context, req commandsrpc.DashboardRequest, id uint64) (commandsrpc.DashboardReply, error) {
	return commandsrpc.DashboardReply{}, d.repo.Delete(ctx, id, req.Name)
}
