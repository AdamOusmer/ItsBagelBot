package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

type delegationRPC struct {
	repo               *repository.Users
	nc                 *nats.Conn
	invalidationPrefix string
	log                *zap.Logger
}

// SubscribeDelegation serves the single-use dashboard-delegation surface under
// the configured prefix. Mirrors SubscribeDashboard's queue-group wiring.
func SubscribeDelegation(nc *nats.Conn, repo *repository.Users, prefix, invalidationPrefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	d := &delegationRPC{repo: repo, nc: nc, invalidationPrefix: invalidationPrefix, log: log}

	type handler struct {
		verb string
		fn   func(context.Context, *nats.Msg)
	}
	verbs := []handler{
		{"create", d.handleCreate},
		{"get", d.handleGet},
		{"consume", d.handleConsume},
		{"list", d.handleList},
		{"revoke", d.handleRevoke},
		{"access", d.handleAccess},
		{"opt_out", d.handleOptOut},
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

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (d *delegationRPC) handleCreate(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.CreateDelegationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	ownerID, err := strconv.ParseUint(req.OwnerUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "owner_user_id must be numeric"})
		return
	}
	if len(req.Sections) == 0 {
		bus.Respond(msg, map[string]any{"error": "at least one section required"})
		return
	}

	token, err := newToken()
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "token generation failed"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// No expiry: the link stays valid until the invitee accepts it (binding them
	// permanently) or the owner revokes it. Access is permanent + revocable, not
	// time-boxed.
	if err := d.repo.CreateDelegation(ctx, token, ownerID, req.OwnerLogin, req.Sections, nil); err != nil {
		d.log.Error("delegation create", zap.Error(err))
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	d.publishInvalidation(ownerID)
	bus.Respond(msg, map[string]any{"token": token})
}

func (d *delegationRPC) handleGet(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.TokenRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	view, err := d.repo.GetDelegation(ctx, req.Token)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "not found"})
		return
	}

	bus.Respond(msg, map[string]any{
		"owner_user_id": strconv.FormatUint(view.OwnerID, 10),
		"owner_login":   view.OwnerLogin,
		"sections":      view.Sections,
		"consumed":      view.Consumed,
	})
}

func (d *delegationRPC) handleConsume(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.ConsumeDelegationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": "bad request"})
		return
	}

	delegateID, err := strconv.ParseUint(req.DelegateUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": "delegate_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	view, err := d.repo.ConsumeDelegation(ctx, req.Token, delegateID, req.DelegateLogin)
	if err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	d.publishInvalidation(view.OwnerID)
	d.publishInvalidation(delegateID)
	bus.Respond(msg, map[string]any{
		"ok":            true,
		"owner_user_id": strconv.FormatUint(view.OwnerID, 10),
		"owner_login":   view.OwnerLogin,
		"sections":      view.Sections,
	})
}

func (d *delegationRPC) handleList(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.OwnerRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	ownerID, err := strconv.ParseUint(req.OwnerUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "owner_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	views, err := d.repo.ListDelegationsByOwner(ctx, ownerID)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	grants := make([]map[string]any, 0, len(views))
	for _, v := range views {
		grants = append(grants, map[string]any{
			"token":          v.Token,
			"sections":       v.Sections,
			"delegate_login": v.DelegateLogin,
			"consumed":       v.Consumed,
		})
	}
	bus.Respond(msg, map[string]any{"grants": grants})
}

func (d *delegationRPC) handleRevoke(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.RevokeDelegationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": "bad request"})
		return
	}

	ownerID, err := strconv.ParseUint(req.OwnerUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": "owner_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := d.repo.RevokeDelegation(ctx, req.Token, ownerID); err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	d.publishInvalidation(ownerID)
	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *delegationRPC) handleAccess(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.AccessRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"error": "bad request"})
		return
	}

	delegateID, err := strconv.ParseUint(req.DelegateUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": "delegate_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	views, err := d.repo.ListAccessByDelegate(ctx, delegateID)
	if err != nil {
		bus.Respond(msg, map[string]any{"error": err.Error()})
		return
	}

	grants := make([]map[string]any, 0, len(views))
	for _, v := range views {
		grants = append(grants, map[string]any{
			"owner_user_id": strconv.FormatUint(v.OwnerID, 10),
			"owner_login":   v.OwnerLogin,
			"sections":      v.Sections,
		})
	}
	bus.Respond(msg, map[string]any{"grants": grants})
}

func (d *delegationRPC) handleOptOut(ctx context.Context, msg *nats.Msg) {
	var req usersrpc.OptOutDelegationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": "bad request"})
		return
	}

	ownerID, err := strconv.ParseUint(req.OwnerUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": "owner_user_id must be numeric"})
		return
	}
	delegateID, err := strconv.ParseUint(req.DelegateUserID, 10, 64)
	if err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": "delegate_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := d.repo.OptOutDelegation(ctx, ownerID, delegateID); err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	d.publishInvalidation(ownerID)
	d.publishInvalidation(delegateID)
	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *delegationRPC) publishInvalidation(id uint64) {
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, "delegation", fmt.Sprint(id)); err != nil {
		d.log.Warn("delegation cache invalidation publish failed", zap.Uint64("broadcaster_id", id), zap.Error(err))
	}
}
