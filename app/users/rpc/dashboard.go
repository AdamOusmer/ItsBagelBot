package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/pkg/bus"
)

type dashboardRPC struct {
	repo                *repository.Users
	nc                  *nats.Conn
	invalidationSubject string
	log                 *zap.Logger
}

func SubscribeDashboard(nc *nats.Conn, repo *repository.Users, prefix, invalidationSubject, queueGroup string, log *zap.Logger) error {
	d := &dashboardRPC{
		repo:                repo,
		nc:                  nc,
		invalidationSubject: invalidationSubject,
		log:                 log,
	}

	type handler struct {
		verb string
		fn   func(*nats.Msg)
	}
	verbs := []handler{
		{"upsert_user", d.handleUpsertUser},
		{"grant_save", d.handleGrantSave},
		{"grant_has", d.handleGrantHas},
		{"active_set", d.handleActiveSet},
		{"active_get", d.handleActiveGet},
		{"status_get", d.handleStatusGet},
		{"state_get", d.handleStateGet},
		{"delete_self", d.handleDeleteSelf},
	}
	for _, h := range verbs {
		subject := prefix + "." + h.verb
		if _, err := nc.QueueSubscribe(subject, queueGroup, h.fn); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

type upsertUserRequest struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

func (d *dashboardRPC) handleUpsertUser(msg *nats.Msg) {
	var req upsertUserRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Ensure email is generated uniquely since we don't fetch it from Twitch by default
	email := fmt.Sprintf("%d@twitch.tv", id)

	if err := d.repo.Register(ctx, id, req.Username, email); err != nil {
		d.log.Error("upsert_user register", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	bus.Respond(msg, map[string]any{"ok": true})
}

type grantSaveRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token"`
}

func (d *dashboardRPC) handleGrantSave(msg *nats.Msg) {
	var req grantSaveRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := d.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch, []byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		d.log.Error("grant_save upsert token", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	// Invalidate cached state for this broadcaster.
	body, _ := json.Marshal(map[string]string{"broadcaster_id": req.BroadcasterUserID})
	if err := d.nc.Publish(d.invalidationSubject, body); err != nil {
		d.log.Warn("grant_save invalidation publish failed", zap.Error(err))
	}

	bus.Respond(msg, map[string]any{"ok": true})
}

type grantHasRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

func (d *dashboardRPC) handleGrantHas(msg *nats.Msg) {
	var req grantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	accessToken, _, err := d.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)

	hasGrant := err == nil && len(accessToken) > 0

	bus.Respond(msg, map[string]any{"has_grant": hasGrant})
}

type activeSetRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Active            bool   `json:"active"`
}

// handleActiveSet flips the receive toggle. The repository publishes the
// change event, so the projector and ingress converge without extra work;
// the explicit invalidation below covers the dashboard's own grant cache.
func (d *dashboardRPC) handleActiveSet(msg *nats.Msg) {
	var req activeSetRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := d.repo.SetActive(ctx, id, req.Active); err != nil {
		d.log.Error("active_set", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	body, _ := json.Marshal(map[string]string{"broadcaster_id": req.BroadcasterUserID})
	if err := d.nc.Publish(d.invalidationSubject, body); err != nil {
		d.log.Warn("active_set invalidation publish failed", zap.Error(err))
	}

	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *dashboardRPC) handleActiveGet(msg *nats.Msg) {
	var req grantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	view, err := d.repo.Get(ctx, id)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	bus.Respond(msg, map[string]any{"active": view.IsActive})
}

// handleStatusGet returns the broadcaster's billing tier (free/paid/vip) so the
// dashboard can show the account status to the user themselves.
func (d *dashboardRPC) handleStatusGet(msg *nats.Msg) {
	var req grantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	view, err := d.repo.Get(ctx, id)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	bus.Respond(msg, map[string]any{"status": view.Status})
}

// handleStateGet returns both the receive toggle and billing tier in one reply.
// active_get and status_get each load the same user view, so the dashboard's
// page render coalesces them here to spend one round trip and one repo.Get
// instead of two.
func (d *dashboardRPC) handleStateGet(msg *nats.Msg) {
	var req grantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	view, err := d.repo.Get(ctx, id)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	bus.Respond(msg, map[string]any{"active": view.IsActive, "status": view.Status})
}

type deleteSelfRequest struct {
	UserID string `json:"user_id"`
}

// handleDeleteSelf removes the user and every delegation they own. Delegations
// are cleared first so no dangling links survive the deleted user row.
func (d *dashboardRPC) handleDeleteSelf(msg *nats.Msg) {
	var req deleteSelfRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := d.repo.DeleteDelegationsByOwner(ctx, id); err != nil {
		d.log.Error("delete_self delegations", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}
	if err := d.repo.Delete(ctx, id); err != nil {
		d.log.Error("delete_self user", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	bus.Respond(msg, map[string]any{"ok": true})
}
