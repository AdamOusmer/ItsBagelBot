package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"
)

type delegationRPC struct {
	repo               *repository.Users
	nc                 *nats.Conn
	invalidationPrefix string
	log                *zap.Logger
}

// SubscribeDelegation serves the single-use dashboard-delegation surface under
// the configured prefix. Mirrors SubscribeDashboard's queue-group wiring.
func SubscribeDelegation(w Wiring, prefix, invalidationPrefix string) error {
	d := &delegationRPC{repo: w.Repo, nc: w.NC, invalidationPrefix: invalidationPrefix, log: w.Log}

	verbs := map[string]func(context.Context, *nats.Msg){
		"create":  d.handleCreate,
		"get":     d.handleGet,
		"consume": d.handleConsume,
		"list":    d.handleList,
		"revoke":  d.handleRevoke,
		"update":  d.handleUpdate,
		"access":  d.handleAccess,
		"opt_out": d.handleOptOut,
	}
	for verb, fn := range verbs {
		subject := prefix + "." + verb
		fn := fn
		if err := bus.QueueSubscribeRPC(w.NC, subject, w.Queue, tracedHandler(w.App, subject, fn)); err != nil {
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
	req, ok := decodeRequest[usersrpc.CreateDelegationRequest](msg)
	if !ok {
		return
	}
	ownerID, ok := parseWireID(msg, req.OwnerUserID, "owner_user_id")
	if !ok {
		return
	}
	if len(req.Sections) == 0 {
		respondErr(msg, "at least one section required")
		return
	}
	token, err := newToken()
	if err != nil {
		respondErr(msg, "token generation failed")
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	// No expiry: the link stays valid until the invitee accepts it (binding them
	// permanently) or the owner revokes it. Access is permanent + revocable, not
	// time-boxed.
	if err := d.repo.CreateDelegation(ctx, token, ownerID, req.OwnerLogin, req.Sections, nil); err != nil {
		monitor.TxnLogger(ctx, d.log).Error("delegation create", zap.Error(err))
		respondErr(msg, err.Error())
		return
	}

	d.publishInvalidation(ownerID)
	bus.Respond(msg, map[string]any{"token": token})
}

func (d *delegationRPC) handleGet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.TokenRequest](msg)
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	view, err := d.repo.GetDelegation(ctx, req.Token)
	if err != nil {
		respondErr(msg, "not found")
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
	req, ok := decodeRequest[usersrpc.ConsumeDelegationRequest](msg)
	if !ok {
		return
	}
	delegateID, ok := parseWireID(msg, req.DelegateUserID, "delegate_user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
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
	req, ok := decodeRequest[usersrpc.OwnerRequest](msg)
	if !ok {
		return
	}
	ownerID, ok := parseWireID(msg, req.OwnerUserID, "owner_user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	views, err := d.repo.ListDelegationsByOwner(ctx, ownerID)
	if err != nil {
		respondErr(msg, err.Error())
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
	req, ok := decodeRequest[usersrpc.RevokeDelegationRequest](msg)
	if !ok {
		return
	}
	ownerID, ok := parseWireID(msg, req.OwnerUserID, "owner_user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	d.writeThenOK(msg, func() error { return d.repo.RevokeDelegation(ctx, req.Token, ownerID) }, ownerID)
}

// writeThenOK runs a delegation mutation and replies with the ok-shaped result:
// {"ok":false,"error":...} on failure, else {"ok":true} after invalidating each
// affected user's cache.
func (d *delegationRPC) writeThenOK(msg *nats.Msg, write func() error, invalidateIDs ...uint64) {
	if err := write(); err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for _, id := range invalidateIDs {
		d.publishInvalidation(id)
	}
	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *delegationRPC) handleUpdate(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.UpdateDelegationRequest](msg)
	if !ok {
		return
	}
	ownerID, ok := parseWireID(msg, req.OwnerUserID, "owner_user_id")
	if !ok {
		return
	}
	if len(req.Sections) == 0 {
		respondErr(msg, "at least one section required")
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	d.writeThenOK(msg, func() error { return d.repo.UpdateDelegationSections(ctx, req.Token, ownerID, req.Sections) }, ownerID)
}

func (d *delegationRPC) handleAccess(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.AccessRequest](msg)
	if !ok {
		return
	}
	delegateID, ok := parseWireID(msg, req.DelegateUserID, "delegate_user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	views, err := d.repo.ListAccessByDelegate(ctx, delegateID)
	if err != nil {
		respondErr(msg, err.Error())
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
	req, ok := decodeRequest[usersrpc.OptOutDelegationRequest](msg)
	if !ok {
		return
	}
	ownerID, ok := parseWireID(msg, req.OwnerUserID, "owner_user_id")
	if !ok {
		return
	}
	delegateID, ok := parseWireID(msg, req.DelegateUserID, "delegate_user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	d.writeThenOK(msg, func() error { return d.repo.OptOutDelegation(ctx, ownerID, delegateID) }, ownerID, delegateID)
}

func (d *delegationRPC) publishInvalidation(id uint64) {
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, "delegation", fmt.Sprint(id)); err != nil {
		d.log.Warn("delegation cache invalidation publish failed", zap.Uint64("broadcaster_id", id), zap.Error(err))
	}
}
