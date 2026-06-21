package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/commands/repository"
	commandsrpc "ItsBagelBot/internal/domain/rpc/commands"
	"ItsBagelBot/pkg/bus"
)

type dashboardRPC struct {
	repo *repository.Commands
	log  *zap.Logger
}

func SubscribeDashboard(nc *nats.Conn, repo *repository.Commands, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	d := &dashboardRPC{repo: repo, log: log}

	verbs := []struct {
		verb    string
		handler func(context.Context, commandsrpc.DashboardRequest) commandsrpc.DashboardReply
	}{
		{"list", d.handleList},
		{"upsert", d.handleUpsert},
		{"delete", d.handleDelete},
	}

	for _, v := range verbs {
		subject := prefix + "." + v.verb
		if err := bus.QueueSubscribeJSON[commandsrpc.DashboardRequest, commandsrpc.DashboardReply](nc, subject, queueGroup, 2*time.Second, app, log, v.handler); err != nil {
			return err
		}
	}
	return nil
}

func (d *dashboardRPC) parseUserID(req commandsrpc.DashboardRequest) (uint64, bool, commandsrpc.DashboardReply) {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, false, commandsrpc.DashboardReply{Error: "invalid user_id"}
	}
	return id, true, commandsrpc.DashboardReply{}
}

func (d *dashboardRPC) handleList(ctx context.Context, req commandsrpc.DashboardRequest) commandsrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	views, err := d.repo.List(ctx, id)
	if err != nil {
		return commandsrpc.DashboardReply{Error: err.Error()}
	}
	return commandsrpc.DashboardReply{Commands: views}
}

func (d *dashboardRPC) handleUpsert(ctx context.Context, req commandsrpc.DashboardRequest) commandsrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	// allowed_user_id is optional; empty/"0" means no per-user restriction.
	var allowedUserID uint64
	if req.AllowedUserID != "" {
		parsed, err := strconv.ParseUint(req.AllowedUserID, 10, 64)
		if err != nil {
			return commandsrpc.DashboardReply{Error: "invalid allowed_user_id"}
		}
		allowedUserID = parsed
	}

	// A rename updates the existing row's name field in place; a plain edit or
	// create goes through the write-behind upsert.
	rename := req.OriginalName != "" && req.OriginalName != req.Name
	var opErr error
	if rename {
		opErr = d.repo.Rename(ctx, id, req.OriginalName, req.Name, req.Aliases, req.Response, req.IsActive, req.StreamOnlineOnly, req.Perm, req.Cooldown, allowedUserID)
	} else {
		opErr = d.repo.Upsert(id, req.Name, req.Aliases, req.Response, req.IsActive, req.StreamOnlineOnly, req.Perm, req.Cooldown, allowedUserID)
	}
	if opErr != nil {
		// Validation/conflict error: return it alongside the current list.
		views, _ := d.repo.List(ctx, id)
		return commandsrpc.DashboardReply{Commands: views, Error: opErr.Error()}
	}

	// Upsert is write-behind (~2 s), so build an optimistic reply.
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return commandsrpc.DashboardReply{Error: err.Error()}
	}

	// Drop the pre-rename key from the optimistic view (rename is immediate, so
	// a fresh list won't carry it, but a cached one might).
	if rename {
		filtered := views[:0]
		for _, v := range views {
			if v.Name != req.OriginalName {
				filtered = append(filtered, v)
			}
		}
		views = filtered
	}

	// Merge the just-written command: replace existing entry or append.
	upserted := repository.CommandView{
		Name:             req.Name,
		Aliases:          req.Aliases,
		Response:         req.Response,
		IsActive:         req.IsActive,
		StreamOnlineOnly: req.StreamOnlineOnly,
		Perm:             req.Perm,
		Cooldown:         req.Cooldown,
		AllowedUserID:    req.AllowedUserID,
	}
	merged := false
	for i, v := range views {
		if v.Name == req.Name {
			views[i] = upserted
			merged = true
			break
		}
	}
	if !merged {
		views = append(views, upserted)
	}

	return commandsrpc.DashboardReply{Commands: views}
}

func (d *dashboardRPC) handleDelete(ctx context.Context, req commandsrpc.DashboardRequest) commandsrpc.DashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	if err := d.repo.Delete(ctx, id, req.Name); err != nil {
		return commandsrpc.DashboardReply{Error: err.Error()}
	}

	// Delete is immediate and invalidates the cache, so List is fresh.
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return commandsrpc.DashboardReply{Error: err.Error()}
	}
	return commandsrpc.DashboardReply{Commands: views}
}
