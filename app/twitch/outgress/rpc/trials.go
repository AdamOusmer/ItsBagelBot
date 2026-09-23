// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	valkey "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// TrialSubscriptionRequest is the versioned ingress-only subscription contract.
// The outgress service verifies every ownership field against Valkey before it
// lets a request use the bot account's user token.
type TrialSubscriptionRequest struct {
	Version         int    `json:"version"`
	BroadcasterID   string `json:"broadcaster_id"`
	SessionID       string `json:"session_id,omitempty"`
	SubscriptionID  string `json:"subscription_id,omitempty"`
	OwnerEpoch      int64  `json:"owner_epoch"`
	TrialGeneration string `json:"trial_generation"`
}

type TrialSubscriptionReply struct {
	SubscriptionID string `json:"subscription_id,omitempty"`
	Deleted        bool   `json:"deleted,omitempty"`
	Error          string `json:"error,omitempty"`
}

type trialSubscriptions struct {
	store  valkey.Client
	twitch *twitch.Client
	botID  string
}

func SubscribeTrialSubscriptions(nc *nats.Conn, store valkey.Client, tw *twitch.Client, botID string, app *newrelic.Application, log *zap.Logger) error {
	h := &trialSubscriptions{store: store, twitch: tw, botID: botID}
	return subscribeAll(
		func() error {
			return bus.QueueSubscribeJSON[TrialSubscriptionRequest, TrialSubscriptionReply](nc, "bagel.rpc.outgress.trial_subscription.create", queueGroupTrial, 5*time.Second, app, log, h.create)
		},
		func() error {
			return bus.QueueSubscribeJSON[TrialSubscriptionRequest, TrialSubscriptionReply](nc, "bagel.rpc.outgress.trial_subscription.delete", queueGroupTrial, 5*time.Second, app, log, h.delete)
		},
	)
}

const queueGroupTrial = "outgress-trial-subscription"

const activateTrial = `
local owner = redis.call('GET', KEYS[1]) or ''
if string.sub(owner, 1, string.len(ARGV[1]) + 1) ~= ARGV[1] .. ':' then return 0 end
if redis.call('GET', KEYS[3]) ~= ARGV[1] .. ':' .. ARGV[4] then return 0 end
if redis.call('HGET', KEYS[2], 'generation') ~= ARGV[2] then return 0 end
local state = redis.call('HGET', KEYS[2], 'state')
if state ~= 'pending' and state ~= 'receiving' and state ~= 'failed' then return 0 end
redis.call('HSET', KEYS[2], 'subscription_id', ARGV[3], 'session_id', ARGV[4], 'owner_epoch', ARGV[1], 'state', 'receiving')
redis.call('HDEL', KEYS[2], 'error')
return 1`

const releaseTrial = `
local owner = redis.call('GET', KEYS[1]) or ''
if string.sub(owner, 1, string.len(ARGV[1]) + 1) ~= ARGV[1] .. ':' then return 0 end
if redis.call('HGET', KEYS[2], 'generation') ~= ARGV[2] then return 0 end
if redis.call('HGET', KEYS[2], 'state') ~= 'stopping' then return 0 end
if (redis.call('HGET', KEYS[2], 'subscription_id') or '') ~= ARGV[3] then return 0 end
redis.call('HDEL', KEYS[2], 'subscription_id', 'session_id', 'owner_epoch')
return 1`

func (h *trialSubscriptions) owned(ctx context.Context, req TrialSubscriptionRequest) (map[string]string, bool) {
	if req.Version != 1 || req.BroadcasterID == "" || req.OwnerEpoch <= 0 || req.TrialGeneration == "" {
		return nil, false
	}
	owner, err := h.store.Do(ctx, h.store.B().Get().Key("trial:owner").Build()).ToString()
	if err != nil || !strings.HasPrefix(owner, fmt.Sprint(req.OwnerEpoch)+":") {
		return nil, false
	}
	fields, err := h.store.Do(ctx, h.store.B().Hgetall().Key("trial:channel:"+req.BroadcasterID).Build()).AsStrMap()
	if err != nil || fields["generation"] != req.TrialGeneration {
		return nil, false
	}
	return fields, true
}

func (h *trialSubscriptions) currentSession(ctx context.Context, req TrialSubscriptionRequest) bool {
	if req.SessionID == "" {
		return false
	}
	session, err := h.store.Do(ctx, h.store.B().Get().Key("trial:owner_session").Build()).ToString()
	return err == nil && session == fmt.Sprint(req.OwnerEpoch)+":"+req.SessionID
}

func (h *trialSubscriptions) create(ctx context.Context, req TrialSubscriptionRequest) TrialSubscriptionReply {
	fields, ok := h.owned(ctx, req)
	if !ok || !h.currentSession(ctx, req) || (fields["state"] != "pending" && fields["state"] != "receiving" && fields["state"] != "failed") || h.botID == "" {
		return TrialSubscriptionReply{Error: "stale_or_invalid_owner"}
	}
	body, _ := codec.Marshal(map[string]any{
		"type": "channel.chat.message", "version": "1",
		"condition": map[string]string{"broadcaster_user_id": req.BroadcasterID, "user_id": h.botID},
		"transport": map[string]string{"method": "websocket", "session_id": req.SessionID},
	})
	res, err := h.twitch.ExecuteAs(ctx, twitch.IdentityBot, "", twitch.HelixCall{Method: http.MethodPost, Endpoint: "/helix/eventsub/subscriptions", Body: body})
	if err != nil {
		return TrialSubscriptionReply{Error: "token_or_twitch_unavailable"}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode != http.StatusConflict {
			return TrialSubscriptionReply{Error: fmt.Sprintf("twitch_%d", res.StatusCode)}
		}
	}
	var created struct {
		ID   string `json:"id"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if codec.Unmarshal(raw, &created) != nil {
		return TrialSubscriptionReply{Error: "invalid_twitch_reply"}
	}
	id := created.ID
	if res.StatusCode == http.StatusConflict {
		if id == "" || !h.existingSubscriptionMatches(ctx, id, req) {
			return TrialSubscriptionReply{Error: "subscription_conflict"}
		}
	} else if len(created.Data) != 0 {
		id = created.Data[0].ID
	}
	if id == "" {
		return TrialSubscriptionReply{Error: "invalid_twitch_reply"}
	}
	key := "trial:channel:" + req.BroadcasterID
	activated, err := h.store.Do(ctx, h.store.B().Eval().Script(activateTrial).Numkeys(3).Key("trial:owner").Key(key).Key("trial:owner_session").Arg(fmt.Sprint(req.OwnerEpoch)).Arg(req.TrialGeneration).Arg(id).Arg(req.SessionID).Build()).AsInt64()
	if err != nil || activated != 1 {
		h.deleteTwitch(ctx, id)
		return TrialSubscriptionReply{Error: "stale_owner_or_valkey_unavailable"}
	}
	go func() {
		lookupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		h.recordDisplayName(lookupCtx, req)
	}()
	return TrialSubscriptionReply{SubscriptionID: id}
}

func (h *trialSubscriptions) existingSubscriptionMatches(ctx context.Context, id string, req TrialSubscriptionRequest) bool {
	res, err := h.twitch.ExecuteAs(ctx, twitch.IdentityBot, "", twitch.HelixCall{Method: http.MethodGet, Endpoint: "/helix/eventsub/subscriptions?subscription_id=" + url.QueryEscape(id)})
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var page struct {
		Data []struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			Condition struct {
				BroadcasterID string `json:"broadcaster_user_id"`
				UserID        string `json:"user_id"`
			} `json:"condition"`
			Transport struct {
				Method    string `json:"method"`
				SessionID string `json:"session_id"`
			} `json:"transport"`
		} `json:"data"`
	}
	if codec.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&page) != nil || len(page.Data) != 1 {
		return false
	}
	sub := page.Data[0]
	return sub.ID == id && sub.Type == "channel.chat.message" && sub.Condition.BroadcasterID == req.BroadcasterID &&
		sub.Condition.UserID == h.botID && sub.Transport.Method == "websocket" && sub.Transport.SessionID == req.SessionID
}

func (h *trialSubscriptions) recordDisplayName(ctx context.Context, req TrialSubscriptionRequest) {
	res, err := h.twitch.ExecuteAs(ctx, twitch.IdentityBot, "", twitch.HelixCall{Method: http.MethodGet, Endpoint: "/helix/users?id=" + url.QueryEscape(req.BroadcasterID)})
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return
	}
	var page struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if codec.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&page) != nil || len(page.Data) != 1 || page.Data[0].ID != req.BroadcasterID || page.Data[0].DisplayName == "" {
		return
	}
	const script = `if redis.call('HGET', KEYS[1], 'generation') ~= ARGV[1] then return 0 end
redis.call('HSET', KEYS[1], 'display_name', ARGV[2]); return 1`
	_ = h.store.Do(ctx, h.store.B().Eval().Script(script).Numkeys(1).Key("trial:channel:"+req.BroadcasterID).Arg(req.TrialGeneration).Arg(page.Data[0].DisplayName).Build()).Error()
}

func (h *trialSubscriptions) delete(ctx context.Context, req TrialSubscriptionRequest) TrialSubscriptionReply {
	fields, ok := h.owned(ctx, req)
	if !ok || fields["state"] != "stopping" || req.SubscriptionID != fields["subscription_id"] {
		return TrialSubscriptionReply{Error: "stale_or_invalid_owner"}
	}
	if req.SubscriptionID != "" {
		if err := h.deleteTwitch(ctx, req.SubscriptionID); err != nil {
			return TrialSubscriptionReply{Error: "twitch_unavailable"}
		}
	}
	key := "trial:channel:" + req.BroadcasterID
	released, err := h.store.Do(ctx, h.store.B().Eval().Script(releaseTrial).Numkeys(2).Key("trial:owner").Key(key).Arg(fmt.Sprint(req.OwnerEpoch)).Arg(req.TrialGeneration).Arg(req.SubscriptionID).Build()).AsInt64()
	if err != nil || released != 1 {
		return TrialSubscriptionReply{Error: "stale_owner_or_valkey_unavailable"}
	}
	return TrialSubscriptionReply{Deleted: true}
}

func (h *trialSubscriptions) deleteTwitch(ctx context.Context, id string) error {
	res, err := h.twitch.ExecuteAs(ctx, twitch.IdentityBot, "", twitch.HelixCall{Method: http.MethodDelete, Endpoint: "/helix/eventsub/subscriptions?id=" + id})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("twitch status %d", res.StatusCode)
}
