package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/commands/repository"
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
		handler func(context.Context, dashboardRequest) dashboardReply
	}{
		{"list", d.handleList},
		{"upsert", d.handleUpsert},
		{"delete", d.handleDelete},
	}

	for _, v := range verbs {
		subject := prefix + "." + v.verb
		if err := bus.QueueSubscribeJSON[dashboardRequest, dashboardReply](nc, subject, queueGroup, 2*time.Second, app, log, v.handler); err != nil {
			return err
		}
	}
	return nil
}

// dashboardRequest covers all three verbs; unused fields are zero-valued.
type dashboardRequest struct {
	UserID           string `json:"user_id"`
	Name             string `json:"name"`
	Response         string `json:"response"`
	IsActive         bool   `json:"is_active"`
	StreamOnlineOnly bool   `json:"stream_online_only"`
	Perm             string `json:"perm"`
	Cooldown         uint   `json:"cooldown"`
	AllowedUserID    string `json:"allowed_user_id"`
	// OriginalName, when set and different from Name, makes upsert a rename:
	// the existing row keeps its identity and its name field is updated in
	// place instead of being deleted and recreated under the new name.
	OriginalName string `json:"original_name"`
}

type dashboardReply struct {
	Commands []repository.CommandView `json:"commands"`
	Error    string                   `json:"error,omitempty"`
}

func (d *dashboardRPC) parseUserID(req dashboardRequest) (uint64, bool, dashboardReply) {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, false, dashboardReply{Error: "invalid user_id"}
	}
	return id, true, dashboardReply{}
}

func (d *dashboardRPC) handleList(ctx context.Context, req dashboardRequest) dashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	views, err := d.repo.List(ctx, id)
	if err != nil {
		return dashboardReply{Error: err.Error()}
	}
	return dashboardReply{Commands: views}
}

func (d *dashboardRPC) handleUpsert(ctx context.Context, req dashboardRequest) dashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	// allowed_user_id is optional; empty/"0" means no per-user restriction.
	var allowedUserID uint64
	if req.AllowedUserID != "" {
		parsed, err := strconv.ParseUint(req.AllowedUserID, 10, 64)
		if err != nil {
			return dashboardReply{Error: "invalid allowed_user_id"}
		}
		allowedUserID = parsed
	}

	// A rename updates the existing row's name field in place; a plain edit or
	// create goes through the write-behind upsert.
	rename := req.OriginalName != "" && req.OriginalName != req.Name
	var opErr error
	if rename {
		opErr = d.repo.Rename(ctx, id, req.OriginalName, req.Name, req.Response, req.IsActive, req.StreamOnlineOnly, req.Perm, req.Cooldown, allowedUserID)
	} else {
		opErr = d.repo.Upsert(id, req.Name, req.Response, req.IsActive, req.StreamOnlineOnly, req.Perm, req.Cooldown, allowedUserID)
	}
	if opErr != nil {
		// Validation/conflict error: return it alongside the current list.
		views, _ := d.repo.List(ctx, id)
		return dashboardReply{Commands: views, Error: opErr.Error()}
	}

	// Upsert is write-behind (~2 s), so build an optimistic reply.
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return dashboardReply{Error: err.Error()}
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

	return dashboardReply{Commands: views}
}

func (d *dashboardRPC) handleDelete(ctx context.Context, req dashboardRequest) dashboardReply {
	id, ok, reply := d.parseUserID(req)
	if !ok {
		return reply
	}

	if err := d.repo.Delete(ctx, id, req.Name); err != nil {
		return dashboardReply{Error: err.Error()}
	}

	// Delete is immediate and invalidates the cache, so List is fresh.
	views, err := d.repo.List(ctx, id)
	if err != nil {
		return dashboardReply{Error: err.Error()}
	}
	return dashboardReply{Commands: views}
}
