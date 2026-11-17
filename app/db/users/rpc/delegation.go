// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"
)

type delegationRPC struct {
	repo          *repository.Users
	invalidations *bus.Invalidations
	log           *zap.Logger
}

func SubscribeDelegation(w Wiring, prefix string) error {
	d := &delegationRPC{repo: w.Repo, invalidations: w.Repo.Invalidations(), log: w.Log}

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
	return serveMsgVerbs(w.Within(dashboardTimeout), prefix, verbs)
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

	if err := d.repo.CreateDelegation(ctx, repository.DelegationLink{Token: token, OwnerID: ownerID, OwnerLogin: req.OwnerLogin, Sections: req.Sections}); err != nil {
		monitor.TxnLogger(ctx, d.log).Error("delegation create", zap.Error(err))
		respondErr(msg, err.Error())
		return
	}

	d.invalidations.Publish(invalidate.ScopeDelegation, ownerID)
	bus.Respond(msg, map[string]any{"token": token})
}

func (d *delegationRPC) handleGet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.TokenRequest](msg)
	if !ok {
		return
	}

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

	view, err := d.repo.ConsumeDelegation(ctx, req.Token, delegateID, req.DelegateLogin)
	if err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	d.invalidations.Publish(invalidate.ScopeDelegation, view.OwnerID)
	d.invalidations.Publish(invalidate.ScopeDelegation, delegateID)
	bus.Respond(msg, map[string]any{
		"ok":            true,
		"owner_user_id": strconv.FormatUint(view.OwnerID, 10),
		"owner_login":   view.OwnerLogin,
		"sections":      view.Sections,
	})
}

func (d *delegationRPC) handleList(ctx context.Context, msg *nats.Msg) {
	d.handleDelegationList(ctx, msg, false)
}

func (d *delegationRPC) handleRevoke(ctx context.Context, msg *nats.Msg) {
	d.handleGrantChange(ctx, msg, true)
}

func (d *delegationRPC) writeThenOK(msg *nats.Msg, write func() error, invalidateIDs ...uint64) {
	if err := write(); err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for _, id := range invalidateIDs {
		d.invalidations.Publish(invalidate.ScopeDelegation, id)
	}
	bus.Respond(msg, map[string]any{"ok": true})
}

func (d *delegationRPC) handleUpdate(ctx context.Context, msg *nats.Msg) {
	d.handleGrantChange(ctx, msg, false)
}

func (d *delegationRPC) handleGrantChange(ctx context.Context, msg *nats.Msg, revoke bool) {
	req, ok := decodeRequest[usersrpc.UpdateDelegationRequest](msg)
	if !ok {
		return
	}
	ownerID, ok := parseWireID(msg, req.OwnerUserID, "owner_user_id")
	if !ok {
		return
	}
	if !revoke && len(req.Sections) == 0 {
		respondErr(msg, "at least one section required")
		return
	}

	if revoke {
		delegateID, err := d.repo.RevokeDelegation(ctx, req.Token, ownerID)
		d.respondGrantChange(msg, grantChangeResult{ownerID, delegateID, err})
		return
	}
	delegateID, err := d.repo.UpdateDelegationSections(ctx, req.Token, ownerID, req.Sections)
	d.respondGrantChange(msg, grantChangeResult{ownerID, delegateID, err})
}

type grantChangeResult struct {
	ownerID, delegateID uint64
	err                 error
}

func (d *delegationRPC) respondGrantChange(msg *nats.Msg, result grantChangeResult) {
	delegateID, err := result.delegateID, result.err
	if err != nil {
		bus.Respond(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	d.invalidations.Publish(invalidate.ScopeDelegation, result.ownerID)
	res := map[string]any{"ok": true}
	if delegateID != 0 {
		d.invalidations.Publish(invalidate.ScopeDelegation, delegateID)
		res["delegate_user_id"] = strconv.FormatUint(delegateID, 10)
	}
	bus.Respond(msg, res)
}

func (d *delegationRPC) handleAccess(ctx context.Context, msg *nats.Msg) {
	d.handleDelegationList(ctx, msg, true)
}

func (d *delegationRPC) handleDelegationList(ctx context.Context, msg *nats.Msg, access bool) {
	req, ok := decodeRequest[struct {
		OwnerUserID    string `json:"owner_user_id"`
		DelegateUserID string `json:"delegate_user_id"`
	}](msg)
	if !ok {
		return
	}
	id, field, query := req.OwnerUserID, "owner_user_id", d.repo.ListDelegationsByOwner
	if access {
		id, field, query = req.DelegateUserID, "delegate_user_id", d.repo.ListAccessByDelegate
	}
	userID, ok := parseWireID(msg, id, field)
	if !ok {
		return
	}

	views, err := query(ctx, userID)
	if err != nil {
		respondErr(msg, err.Error())
		return
	}

	grants := make([]map[string]any, 0, len(views))
	for _, v := range views {
		grants = append(grants, delegationGrant(v, access))
	}
	bus.Respond(msg, map[string]any{"grants": grants})
}

func delegationGrant(v repository.DelegationView, access bool) map[string]any {
	if access {
		return map[string]any{"owner_user_id": strconv.FormatUint(v.OwnerID, 10), "owner_login": v.OwnerLogin, "sections": v.Sections}
	}
	return map[string]any{"token": v.Token, "sections": v.Sections, "delegate_login": v.DelegateLogin, "consumed": v.Consumed}
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

	d.writeThenOK(msg, func() error { return d.repo.OptOutDelegation(ctx, ownerID, delegateID) }, ownerID, delegateID)
}
