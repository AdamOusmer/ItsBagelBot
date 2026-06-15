package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/commands/repository"
)

type dashboardRPC struct {
	repo *repository.Commands
	log  *zap.Logger
}

func SubscribeDashboard(nc *nats.Conn, repo *repository.Commands, prefix, queueGroup string, log *zap.Logger) error {
	d := &dashboardRPC{repo: repo, log: log}

	verbs := []struct {
		verb    string
		handler nats.MsgHandler
	}{
		{"list", d.handleList},
		{"upsert", d.handleUpsert},
		{"delete", d.handleDelete},
	}

	for _, v := range verbs {
		subject := prefix + "." + v.verb
		if _, err := nc.QueueSubscribe(subject, queueGroup, v.handler); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

// dashboardRequest covers all three verbs; unused fields are zero-valued.
type dashboardRequest struct {
	UserID        string `json:"user_id"`
	Name          string `json:"name"`
	Response      string `json:"response"`
	IsActive      bool   `json:"is_active"`
	Perm          string `json:"perm"`
	Cooldown      uint   `json:"cooldown"`
	AllowedUserID string `json:"allowed_user_id"`
}

type dashboardReply struct {
	Commands []repository.CommandView `json:"commands"`
	Error    string                   `json:"error,omitempty"`
}

func respondDash(msg *nats.Msg, reply dashboardReply) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
}

func (d *dashboardRPC) parseUserID(msg *nats.Msg) (uint64, *dashboardRequest, bool) {
	var req dashboardRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDash(msg, dashboardReply{Error: "bad request"})
		return 0, nil, false
	}
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		respondDash(msg, dashboardReply{Error: "invalid user_id"})
		return 0, nil, false
	}
	return id, &req, true
}

func (d *dashboardRPC) handleList(msg *nats.Msg) {
	id, _, ok := d.parseUserID(msg)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	views, err := d.repo.List(ctx, id)
	if err != nil {
		respondDash(msg, dashboardReply{Error: err.Error()})
		return
	}
	respondDash(msg, dashboardReply{Commands: views})
}

func (d *dashboardRPC) handleUpsert(msg *nats.Msg) {
	id, req, ok := d.parseUserID(msg)
	if !ok {
		return
	}

	// allowed_user_id is optional; empty/"0" means no per-user restriction.
	var allowedUserID uint64
	if req.AllowedUserID != "" {
		parsed, err := strconv.ParseUint(req.AllowedUserID, 10, 64)
		if err != nil {
			respondDash(msg, dashboardReply{Error: "invalid allowed_user_id"})
			return
		}
		allowedUserID = parsed
	}

	if err := d.repo.Upsert(id, req.Name, req.Response, req.IsActive, req.Perm, req.Cooldown, allowedUserID); err != nil {
		// Validation error: return it alongside the current (unmodified) list.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		views, _ := d.repo.List(ctx, id)
		respondDash(msg, dashboardReply{Commands: views, Error: err.Error()})
		return
	}

	// Upsert is write-behind (~2 s), so build an optimistic reply.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	views, err := d.repo.List(ctx, id)
	if err != nil {
		respondDash(msg, dashboardReply{Error: err.Error()})
		return
	}

	// Merge the just-upserted command: replace existing entry or append.
	upserted := repository.CommandView{
		Name:          req.Name,
		Response:      req.Response,
		IsActive:      req.IsActive,
		Perm:          req.Perm,
		Cooldown:      req.Cooldown,
		AllowedUserID: req.AllowedUserID,
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

	respondDash(msg, dashboardReply{Commands: views})
}

func (d *dashboardRPC) handleDelete(msg *nats.Msg) {
	id, req, ok := d.parseUserID(msg)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := d.repo.Delete(ctx, id, req.Name); err != nil {
		respondDash(msg, dashboardReply{Error: err.Error()})
		return
	}

	// Delete is immediate and invalidates the cache, so List is fresh.
	views, err := d.repo.List(ctx, id)
	if err != nil {
		respondDash(msg, dashboardReply{Error: err.Error()})
		return
	}
	respondDash(msg, dashboardReply{Commands: views})
}
