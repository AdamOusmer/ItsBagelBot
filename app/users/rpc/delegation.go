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
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
)

type delegationRPC struct {
	repo *repository.Users
	log  *zap.Logger
}

// SubscribeDelegation serves the single-use dashboard-delegation surface under
// the configured prefix. Mirrors SubscribeDashboard's queue-group wiring.
func SubscribeDelegation(nc *nats.Conn, repo *repository.Users, prefix, queueGroup string, log *zap.Logger) error {
	d := &delegationRPC{repo: repo, log: log}

	type handler struct {
		verb string
		fn   func(*nats.Msg)
	}
	verbs := []handler{
		{"create", d.handleCreate},
		{"get", d.handleGet},
		{"consume", d.handleConsume},
		{"list", d.handleList},
		{"revoke", d.handleRevoke},
		{"access", d.handleAccess},
	}
	for _, h := range verbs {
		subject := prefix + "." + h.verb
		if _, err := nc.QueueSubscribe(subject, queueGroup, h.fn); err != nil {
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

type createDelegationRequest struct {
	OwnerUserID string   `json:"owner_user_id"`
	OwnerLogin  string   `json:"owner_login"`
	Sections    []string `json:"sections"`
}

func (d *delegationRPC) handleCreate(msg *nats.Msg) {
	var req createDelegationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDeleg(msg, map[string]any{"error": "bad request"})
		return
	}

	ownerID, err := strconv.ParseUint(req.OwnerUserID, 10, 64)
	if err != nil {
		respondDeleg(msg, map[string]any{"error": "owner_user_id must be numeric"})
		return
	}
	if len(req.Sections) == 0 {
		respondDeleg(msg, map[string]any{"error": "at least one section required"})
		return
	}

	token, err := newToken()
	if err != nil {
		respondDeleg(msg, map[string]any{"error": "token generation failed"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := d.repo.CreateDelegation(ctx, token, ownerID, req.OwnerLogin, req.Sections, nil); err != nil {
		d.log.Error("delegation create", zap.Error(err))
		respondDeleg(msg, map[string]any{"error": err.Error()})
		return
	}

	respondDeleg(msg, map[string]any{"token": token})
}

type tokenRequest struct {
	Token string `json:"token"`
}

func (d *delegationRPC) handleGet(msg *nats.Msg) {
	var req tokenRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDeleg(msg, map[string]any{"error": "bad request"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	view, err := d.repo.GetDelegation(ctx, req.Token)
	if err != nil {
		respondDeleg(msg, map[string]any{"error": "not found"})
		return
	}

	respondDeleg(msg, map[string]any{
		"owner_user_id": strconv.FormatUint(view.OwnerID, 10),
		"owner_login":   view.OwnerLogin,
		"sections":      view.Sections,
		"consumed":      view.Consumed,
	})
}

type consumeDelegationRequest struct {
	Token          string `json:"token"`
	DelegateUserID string `json:"delegate_user_id"`
	DelegateLogin  string `json:"delegate_login"`
}

func (d *delegationRPC) handleConsume(msg *nats.Msg) {
	var req consumeDelegationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDeleg(msg, map[string]any{"ok": false, "error": "bad request"})
		return
	}

	delegateID, err := strconv.ParseUint(req.DelegateUserID, 10, 64)
	if err != nil {
		respondDeleg(msg, map[string]any{"ok": false, "error": "delegate_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	view, err := d.repo.ConsumeDelegation(ctx, req.Token, delegateID, req.DelegateLogin)
	if err != nil {
		respondDeleg(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	respondDeleg(msg, map[string]any{
		"ok":            true,
		"owner_user_id": strconv.FormatUint(view.OwnerID, 10),
		"owner_login":   view.OwnerLogin,
		"sections":      view.Sections,
	})
}

type ownerRequest struct {
	OwnerUserID string `json:"owner_user_id"`
}

func (d *delegationRPC) handleList(msg *nats.Msg) {
	var req ownerRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDeleg(msg, map[string]any{"error": "bad request"})
		return
	}

	ownerID, err := strconv.ParseUint(req.OwnerUserID, 10, 64)
	if err != nil {
		respondDeleg(msg, map[string]any{"error": "owner_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	views, err := d.repo.ListDelegationsByOwner(ctx, ownerID)
	if err != nil {
		respondDeleg(msg, map[string]any{"error": err.Error()})
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
	respondDeleg(msg, map[string]any{"grants": grants})
}

type revokeDelegationRequest struct {
	OwnerUserID string `json:"owner_user_id"`
	Token       string `json:"token"`
}

func (d *delegationRPC) handleRevoke(msg *nats.Msg) {
	var req revokeDelegationRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDeleg(msg, map[string]any{"ok": false, "error": "bad request"})
		return
	}

	ownerID, err := strconv.ParseUint(req.OwnerUserID, 10, 64)
	if err != nil {
		respondDeleg(msg, map[string]any{"ok": false, "error": "owner_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := d.repo.RevokeDelegation(ctx, req.Token, ownerID); err != nil {
		respondDeleg(msg, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	respondDeleg(msg, map[string]any{"ok": true})
}

type accessRequest struct {
	DelegateUserID string `json:"delegate_user_id"`
}

func (d *delegationRPC) handleAccess(msg *nats.Msg) {
	var req accessRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDeleg(msg, map[string]any{"error": "bad request"})
		return
	}

	delegateID, err := strconv.ParseUint(req.DelegateUserID, 10, 64)
	if err != nil {
		respondDeleg(msg, map[string]any{"error": "delegate_user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	views, err := d.repo.ListAccessByDelegate(ctx, delegateID)
	if err != nil {
		respondDeleg(msg, map[string]any{"error": err.Error()})
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
	respondDeleg(msg, map[string]any{"grants": grants})
}

func respondDeleg(msg *nats.Msg, v any) {
	body, _ := json.Marshal(v)
	_ = msg.Respond(body)
}
