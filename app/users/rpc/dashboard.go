package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

type dashboardRPC struct {
	repo               *repository.Users
	nc                 *nats.Conn
	invalidationPrefix string
	log                *zap.Logger
}

func SubscribeDashboard(nc *nats.Conn, repo *repository.Users, prefix, invalidationPrefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	d := &dashboardRPC{
		repo:               repo,
		nc:                 nc,
		invalidationPrefix: invalidationPrefix,
		log:                log,
	}

	type handler struct {
		verb string
		fn   func(context.Context, *nats.Msg)
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
		fn := h.fn
		if _, err := nc.QueueSubscribe(subject, queueGroup, func(msg *nats.Msg) {
			txn := app.StartTransaction("rpc " + subject)
			defer txn.End()
			ctx := newrelic.NewContext(context.Background(), txn)
			fn(ctx, msg)
		}); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

func (d *dashboardRPC) handleUpsertUser(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.UpsertUserRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Ensure email is generated uniquely since we don't fetch it from Twitch by default
	email := fmt.Sprintf("%d@twitch.tv", id)

	if err := d.repo.Register(ctx, id, req.Username, email); err != nil {
		d.log.Error("upsert_user register", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	// Push-drop cached account state on every console replica: a recreated
	// account must not keep serving another pod's deleted-era view for the
	// rest of that pod's SWR window.
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, "status", req.UserID); err != nil {
		d.log.Warn("upsert_user invalidation publish failed", zap.Error(err))
	}

	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *dashboardRPC) handleGrantSave(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.GrantSaveRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := d.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch, []byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		d.log.Error("grant_save upsert token", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	// Invalidate cached state for this broadcaster.
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, "grant", req.BroadcasterUserID); err != nil {
		d.log.Warn("grant_save invalidation publish failed", zap.Error(err))
	}

	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *dashboardRPC) handleGrantHas(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.GrantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	accessToken, _, err := d.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)

	hasGrant := err == nil && len(accessToken) > 0

	bus.Respond(msg, map[string]any{"has_grant": hasGrant})
}

// handleActiveSet flips the receive toggle. The repository publishes the
// change event, so the projector and ingress converge without extra work;
// the explicit invalidation below covers the dashboard's own grant cache.
func (d *dashboardRPC) handleActiveSet(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.ActiveSetRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := d.repo.SetActive(ctx, id, req.Active); err != nil {
		d.log.Error("active_set", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	if err := invalidate.Publish(d.nc, d.invalidationPrefix, "status", req.BroadcasterUserID); err != nil {
		d.log.Warn("active_set invalidation publish failed", zap.Error(err))
	}

	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *dashboardRPC) handleActiveGet(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.GrantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
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
func (d *dashboardRPC) handleStatusGet(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.GrantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
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
func (d *dashboardRPC) handleStateGet(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.GrantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.BroadcasterUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "broadcaster_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	view, err := d.repo.Get(ctx, id)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	bus.Respond(msg, map[string]any{"active": view.IsActive, "status": view.Status})
}

// handleDeleteSelf removes the user and every delegation they own. Delegations
// are cleared first so no dangling links survive the deleted user row.
func (d *dashboardRPC) handleDeleteSelf(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.DeleteSelfRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
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

	// "user" is not a routed scope on the console side, so it falls through to
	// the '*' entry: a coarse per-user flush of every cached prefix. That is
	// exactly right for deletion — no replica may keep any view of this user,
	// and without this ping other pods would serve stale state for the rest of
	// their SWR windows (the deleting pod only drops its own L1).
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, "user", req.UserID); err != nil {
		d.log.Warn("delete_self invalidation publish failed", zap.Error(err))
	}

	bus.Respond(msg, map[string]any{"ok": true})
}
