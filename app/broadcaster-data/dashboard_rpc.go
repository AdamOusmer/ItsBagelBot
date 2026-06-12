// Dashboard RPC verbs. These handle data requests from the dashboard service
// over NATS so it doesn't need its own MySQL store: user upserts, bot-grant
// storage, and grant existence checks all route here.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/internal/db/ent"
	"ItsBagelBot/internal/db/ent/botgrants"
	"ItsBagelBot/internal/db/ent/user"
)

type dashboardRPC struct {
	db                  *ent.Client
	nc                  *nats.Conn
	invalidationSubject string
	log                 *zap.Logger
}

// subscribe registers the dashboard verbs under prefix, all on the service
// queue group so one replica answers.
func (d *dashboardRPC) subscribe(prefix string) error {
	type handler struct {
		verb string
		fn   func(*nats.Msg)
	}
	verbs := []handler{
		{"upsert_user", d.handleUpsertUser},
		{"grant_save", d.handleGrantSave},
		{"grant_has", d.handleGrantHas},
	}
	for _, h := range verbs {
		subject := prefix + "." + h.verb
		if _, err := d.nc.QueueSubscribe(subject, rpcQueueGroup, h.fn); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// upsert_user
// ---------------------------------------------------------------------------

type upsertUserRequest struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

func (d *dashboardRPC) handleUpsertUser(msg *nats.Msg) {
	var req upsertUserRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDash(msg, map[string]any{"error": "bad request"})
		return
	}

	var id uint64
	if _, err := fmt.Sscanf(req.UserID, "%d", &id); err != nil {
		respondDash(msg, map[string]any{"error": "user_id must be numeric"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	existing, err := d.db.User.Query().Where(user.ID(id)).Only(ctx)
	if ent.IsNotFound(err) {
		// Create new user.
		_, err = d.db.User.Create().
			SetID(id).
			SetUsername(req.Username).
			SetDisplayName(req.DisplayName).
			SetEmail(fmt.Sprintf("%d@twitch.tv", id)).
			Save(ctx)
		if err != nil {
			d.log.Error("upsert_user create", zap.Error(err))
			respondDash(msg, map[string]any{"error": err.Error()})
			return
		}
		respondDash(msg, map[string]any{"ok": true})
		return
	}
	if err != nil {
		d.log.Error("upsert_user query", zap.Error(err))
		respondDash(msg, map[string]any{"error": err.Error()})
		return
	}

	// Update only if something changed.
	upd := d.db.User.UpdateOneID(existing.ID)
	dirty := false
	if req.Username != "" && req.Username != existing.Username {
		upd.SetUsername(req.Username)
		dirty = true
	}
	if req.DisplayName != existing.DisplayName {
		upd.SetDisplayName(req.DisplayName)
		dirty = true
	}
	if dirty {
		if err := upd.Exec(ctx); err != nil {
			d.log.Error("upsert_user update", zap.Error(err))
			respondDash(msg, map[string]any{"error": err.Error()})
			return
		}
	}
	respondDash(msg, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// grant_save
// ---------------------------------------------------------------------------

type grantSaveRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
	Scopes            string `json:"scopes"`
	RefreshTokenEnc   string `json:"refresh_token_enc"` // base64-encoded
}

func (d *dashboardRPC) handleGrantSave(msg *nats.Msg) {
	var req grantSaveRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDash(msg, map[string]any{"error": "bad request"})
		return
	}

	tokenBytes, err := base64.StdEncoding.DecodeString(req.RefreshTokenEnc)
	if err != nil {
		respondDash(msg, map[string]any{"error": "refresh_token_enc: invalid base64"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	existing, err := d.db.BotGrants.Query().
		Where(botgrants.BroadcasterUserIDEQ(req.BroadcasterUserID)).
		Only(ctx)

	if ent.IsNotFound(err) {
		_, err = d.db.BotGrants.Create().
			SetBroadcasterUserID(req.BroadcasterUserID).
			SetScopes(req.Scopes).
			SetRefreshTokenEnc(tokenBytes).
			Save(ctx)
		if err != nil {
			d.log.Error("grant_save create", zap.Error(err))
			respondDash(msg, map[string]any{"error": err.Error()})
			return
		}
	} else if err != nil {
		d.log.Error("grant_save query", zap.Error(err))
		respondDash(msg, map[string]any{"error": err.Error()})
		return
	} else {
		if err := d.db.BotGrants.UpdateOne(existing).
			SetScopes(req.Scopes).
			SetRefreshTokenEnc(tokenBytes).
			Exec(ctx); err != nil {
			d.log.Error("grant_save update", zap.Error(err))
			respondDash(msg, map[string]any{"error": err.Error()})
			return
		}
	}

	// Invalidate cached state for this broadcaster.
	body, _ := json.Marshal(map[string]string{"broadcaster_id": req.BroadcasterUserID})
	if err := d.nc.Publish(d.invalidationSubject, body); err != nil {
		d.log.Warn("grant_save invalidation publish failed", zap.Error(err))
	}

	respondDash(msg, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// grant_has
// ---------------------------------------------------------------------------

type grantHasRequest struct {
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

func (d *dashboardRPC) handleGrantHas(msg *nats.Msg) {
	var req grantHasRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondDash(msg, map[string]any{"error": "bad request"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	exists, err := d.db.BotGrants.Query().
		Where(botgrants.BroadcasterUserIDEQ(req.BroadcasterUserID)).
		Exist(ctx)
	if err != nil {
		d.log.Error("grant_has query", zap.Error(err))
		respondDash(msg, map[string]any{"error": err.Error()})
		return
	}
	respondDash(msg, map[string]any{"has_grant": exists})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func respondDash(msg *nats.Msg, v any) {
	body, _ := json.Marshal(v)
	_ = msg.Respond(body)
}
