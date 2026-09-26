// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const chattersHandleTimeout = 10 * time.Second
const viewerChattersTimeout = 3500 * time.Millisecond
const viewerChattersMaxPages = 30

// ChattersOptions keeps enrollment fencing and the fleet's authoritative quota
// manager explicit. The admission hook must read primary state and compare the
// requested session; an unavailable authority fails closed.
type ChattersOptions struct {
	Limiter              ratelimit.Manager
	AdmitViewer          func(context.Context, string) (bool, error)
	Admit                func(context.Context, manage.ChattersRequest) (bool, error)
	ProviderRetryAt      func(context.Context, string) (time.Time, error)
	ObserveProviderReset func(context.Context, string, time.Time) error
}

type chatterAPI interface {
	GetChattersPage(context.Context, string, string, string) (twitch.ChattersPage, error)
	StreamSession(context.Context, string) (string, time.Time, bool, error)
}
type chatters struct {
	twitch chatterAPI
	botID  string
	log    *zap.Logger
	opts   ChattersOptions
	now    func() time.Time
}

// These shared keys and numeric parameters match worker/buckets.go exactly.
// Attendance has its own bounded share underneath that authority, so a large
// room cannot consume all bot capacity; tenant admission is conservative.
var (
	chatterWatchSpec  = ratelimit.NewSpec(200, 200.0/60)
	chatterLiveSpec   = ratelimit.NewSpec(100, 100.0/60)
	chatterTenantSpec = ratelimit.NewSpec(30, 30.0/60)
)

// SubscribeChatters serves one page per request on a bounded worker pool.
// Absolute caller deadlines include queue time; expired work never calls Twitch.
func SubscribeChatters(nc *nats.Conn, tw *twitch.Client, botID, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger, opts ChattersOptions) error {
	c := &chatters{twitch: tw, botID: botID, log: log, opts: opts, now: time.Now}
	_, err := bus.QueueSubscribeRPCConcurrent(nc, bus.RPCSubscription{
		Subject: prefix + ".chatters.get", QueueGroup: queueGroup,
		Policy: bus.RPCPoolPolicy{MaxWorkers: 4, QueueDepth: 16},
	}, func(msg *nats.Msg) {
		txn := app.StartTransaction("rpc chatters.get")
		defer txn.End()
		ctx := newrelic.NewContext(context.Background(), txn)
		var req manage.ChattersRequest
		var reply manage.ChattersReply
		if err := codec.Unmarshal(msg.Data, &req); err != nil {
			reply = manage.ChattersReply{Error: "bad request", ErrorCode: "invalid"}
		} else {
			reply = c.handleGet(ctx, req)
		}
		body, err := codec.Marshal(reply)
		if err != nil {
			body = []byte(`{"error":"request failed","error_code":"unavailable"}`)
		}
		_ = msg.Respond(body)
	})
	if err != nil {
		return err
	}
	return nc.Flush()
}

func (c *chatters) handleGet(parent context.Context, req manage.ChattersRequest) (reply manage.ChattersReply) {
	if req.RequestID == "" && req.WindowID == "" && req.SessionGeneration == "" && req.LiveSession == "" && req.Cursor == "" && !req.CheckLive {
		return c.handleViewer(parent, req)
	}
	reply = manage.ChattersReply{BroadcasterID: req.BroadcasterID, RequestID: req.RequestID, WindowID: req.WindowID, SessionGeneration: req.SessionGeneration, LiveSession: req.LiveSession}
	fail := func(code string, retryAt time.Time) manage.ChattersReply {
		reply.ErrorCode = code
		reply.Error = "attendance request failed"
		if !retryAt.IsZero() {
			reply.RetryAtUnixMilli = retryAt.UnixMilli()
		}
		reply.MissingScope = code == "authorization"
		if code == "rate_limited" && c.log != nil {
			c.log.Debug("watchtime: attendance quota rejected", zap.String("broadcaster_id", req.BroadcasterID), zap.String("request_id", req.RequestID), zap.String("window_id", req.WindowID), zap.Bool("check_live", req.CheckLive), zap.Int64("retry_at_unix_milli", reply.RetryAtUnixMilli))
		}
		return reply
	}
	now := c.now()
	if req.BroadcasterID == "" || c.botID == "" || req.RequestID == "" || req.WindowID == "" || len(req.BroadcasterID) > 64 || len(req.RequestID) > 128 || len(req.WindowID) > 128 || len(req.Cursor) > 4096 || req.DeadlineUnixMilli <= 0 {
		return fail("invalid", time.Time{})
	}
	if _, err := strconv.ParseUint(req.BroadcasterID, 10, 64); err != nil || req.BroadcasterID == "0" {
		return fail("invalid", time.Time{})
	}
	deadline := time.UnixMilli(req.DeadlineUnixMilli)
	if !deadline.After(now) {
		return fail("expired", time.Time{})
	}
	if max := now.Add(chattersHandleTimeout); deadline.After(max) {
		deadline = max
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return fail("expired", time.Time{})
	}
	if c.opts.Admit == nil || c.opts.Limiter == nil {
		return fail("unavailable", now.Add(time.Second))
	}
	allowed, err := c.opts.Admit(ctx, req)
	if err != nil {
		code, retryAt := chattersFailure(err, c.now())
		return fail(code, retryAt)
	}
	if !allowed {
		return fail("inactive", time.Time{})
	}
	ctx = twitch.WithAttemptAdmission(ctx, c.admit(req))
	ctx = twitch.WithProviderResetObserver(ctx, func(ctx context.Context, endpoint string, reset time.Time) error {
		return c.recordProviderReset(ctx, c.providerIdentity(endpoint), reset)
	})
	if req.CheckLive {
		streamID, startedAt, live, err := c.twitch.StreamSession(ctx, req.BroadcasterID)
		reply.StreamID = streamID
		if !startedAt.IsZero() {
			reply.StreamStartedAtUnixMilli = startedAt.UnixMilli()
		}
		if err != nil {
			code, retryAt := c.providerFailure(ctx, "helix:app", err)
			return fail(code, retryAt)
		}
		reply.Live = live
		reply.CheckedAtUnixMilli = c.now().UnixMilli()
		if !live {
			reply.Complete = true
			return reply
		}
	}
	page, err := c.twitch.GetChattersPage(ctx, req.BroadcasterID, c.botID, req.Cursor)
	if err != nil {
		code, retryAt := c.providerFailure(ctx, "helix:bot:"+c.botID, err)
		return fail(code, retryAt)
	}
	reply.Chatters = make([]manage.Chatter, 0, len(page.Chatters))
	for _, ch := range page.Chatters {
		reply.Chatters = append(reply.Chatters, manage.Chatter{ID: ch.ID, Login: ch.Login})
	}
	reply.NextCursor = page.NextCursor
	reply.Complete = page.Complete
	return reply
}

// The viewer fallback predates correlated watch pages. Return a full bounded
// listing or an error: a partial page must never become an authoritative cache.
func (c *chatters) handleViewer(parent context.Context, req manage.ChattersRequest) manage.ChattersReply {
	reply := manage.ChattersReply{BroadcasterID: req.BroadcasterID}
	fail := func(err error) manage.ChattersReply {
		reply.ErrorCode, _ = chattersFailure(err, c.now())
		reply.Error = "viewer request failed"
		reply.MissingScope = reply.ErrorCode == "authorization"
		reply.Chatters = nil
		return reply
	}
	if id, err := strconv.ParseUint(req.BroadcasterID, 10, 64); err != nil || id == 0 || len(req.BroadcasterID) > 64 || c.botID == "" {
		return fail(&twitch.AdmissionError{Code: "invalid"})
	}
	deadline := c.now().Add(viewerChattersTimeout)
	if req.DeadlineUnixMilli > 0 && time.UnixMilli(req.DeadlineUnixMilli).Before(deadline) {
		deadline = time.UnixMilli(req.DeadlineUnixMilli)
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	if c.opts.Limiter == nil || c.opts.AdmitViewer == nil {
		return fail(&twitch.AdmissionError{Code: "unavailable"})
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	allowed, err := c.admitTenant(ctx, req)
	if err != nil {
		return fail(err)
	}
	if !allowed {
		return fail(&twitch.AdmissionError{Code: "inactive"})
	}
	ctx = twitch.WithAttemptAdmission(ctx, c.admit(req))
	ctx = twitch.WithProviderResetObserver(ctx, func(ctx context.Context, endpoint string, reset time.Time) error {
		return c.recordProviderReset(ctx, c.providerIdentity(endpoint), reset)
	})
	cursor := ""
	seen := map[string]bool{}
	for range viewerChattersMaxPages {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		page, err := c.twitch.GetChattersPage(ctx, req.BroadcasterID, c.botID, cursor)
		if err != nil {
			code, at := c.providerFailure(ctx, "helix:bot:"+c.botID, err)
			return fail(&twitch.AdmissionError{Code: code, RetryAt: at})
		}
		for _, ch := range page.Chatters {
			reply.Chatters = append(reply.Chatters, manage.Chatter{ID: ch.ID, Login: ch.Login})
		}
		if page.Complete {
			reply.Complete = true
			return reply
		}
		if page.NextCursor == "" || page.NextCursor == cursor || seen[page.NextCursor] {
			return fail(twitch.ErrRepeatedCursor)
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return fail(twitch.ErrChattersIncomplete)
}

func (c *chatters) admitTenant(ctx context.Context, req manage.ChattersRequest) (bool, error) {
	if req.RequestID == "" {
		if c.opts.AdmitViewer == nil {
			return false, errors.New("viewer admission unavailable")
		}
		return c.opts.AdmitViewer(ctx, req.BroadcasterID)
	}
	if c.opts.Admit == nil {
		return false, errors.New("watch admission unavailable")
	}
	return c.opts.Admit(ctx, req)
}

func (c *chatters) admit(req manage.ChattersRequest) twitch.AttemptAdmission {
	return func(ctx context.Context, endpoint string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		active, err := c.admitTenant(ctx, req)
		if err != nil {
			return &twitch.AdmissionError{Code: "unavailable", RetryAt: c.now().Add(time.Second)}
		}
		if !active {
			return &twitch.AdmissionError{Code: "inactive"}
		}
		identity := c.providerIdentity(endpoint)
		if c.opts.ProviderRetryAt != nil {
			reset, err := c.opts.ProviderRetryAt(ctx, identity)
			if err != nil {
				return &twitch.AdmissionError{Code: "unavailable", RetryAt: c.now().Add(time.Second)}
			}
			// The provider authority expires resets using server TIME. Do not
			// override its live-reset decision with this pod's wall clock.
			if !reset.IsZero() {
				return &twitch.AdmissionError{Code: "rate_limited", RetryAt: reset}
			}
		}
		tenant := chatterTenantSpec.ForDynamicKey("ratelimit:watch:tenant:", "watch:tenant", req.BroadcasterID)
		allowed, err := c.opts.Limiter.Allow(ctx, tenant)
		if err != nil {
			return &twitch.AdmissionError{Code: "unavailable", RetryAt: c.now().Add(time.Second)}
		}
		if !allowed {
			return &twitch.AdmissionError{Code: "rate_limited", RetryAt: c.now().Add(2 * time.Second)}
		}
		share, shared := chatterWatchSpec.ForKey("ratelimit:watch:chatters"), ratelimit.HelixBotRequest()
		if strings.HasPrefix(endpoint, "/helix/streams?") {
			share = chatterLiveSpec.ForKey("ratelimit:watch:live")
			shared = ratelimit.HelixAppRequest()
		}
		denied, err := c.opts.Limiter.AllowOrdered(ctx, share, shared)
		if err != nil {
			return &twitch.AdmissionError{Code: "unavailable", RetryAt: c.now().Add(time.Second)}
		}
		if denied != 0 {
			return &twitch.AdmissionError{Code: "rate_limited", RetryAt: c.now().Add(time.Second)}
		}
		return nil
	}
}

// providerFailure persists only actual provider feedback; local tenant/share
// denials must not establish a token-wide cooldown.
func (c *chatters) providerFailure(ctx context.Context, identity string, err error) (string, time.Time) {
	var observed *twitch.AdmissionError
	if errors.As(err, &observed) && observed.Provider && !observed.ProviderObserved && c.opts.ObserveProviderReset != nil {
		// Keep the reset even if the caller's deadline expired after HTTP returned.
		if persistErr := c.recordProviderReset(ctx, identity, observed.RetryAt); persistErr != nil {
			return "unavailable", c.now().Add(time.Second)
		}
	}
	return chattersFailure(err, c.now())
}

func (c *chatters) providerIdentity(endpoint string) string {
	if strings.HasPrefix(endpoint, "/helix/streams?") {
		return "helix:app"
	}
	return "helix:bot:" + c.botID
}
func (c *chatters) recordProviderReset(ctx context.Context, identity string, reset time.Time) error {
	if c.opts.ObserveProviderReset == nil {
		return nil
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := c.opts.ObserveProviderReset(persistCtx, identity, reset); err != nil {
		return &twitch.AdmissionError{Code: "unavailable", RetryAt: c.now().Add(time.Second)}
	}
	return nil
}

func chattersFailure(err error, now time.Time) (string, time.Time) {
	var admission *twitch.AdmissionError
	if errors.As(err, &admission) {
		return admission.Code, admission.RetryAt
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "expired", time.Time{}
	}
	if errors.Is(err, twitch.ErrRepeatedCursor) {
		return "repeated_cursor", time.Time{}
	}
	if errors.Is(err, twitch.ErrMissingScope) || errors.Is(err, twitch.ErrNoUserToken) || twitch.GrantDead(err) {
		return "authorization", time.Time{}
	}
	var status *twitch.StatusError
	if errors.As(err, &status) {
		switch status.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "authorization", time.Time{}
		case http.StatusBadRequest:
			return "invalid", time.Time{}
		case http.StatusTooManyRequests:
			return "rate_limited", now.Add(time.Second)
		}
	}
	return "unavailable", now.Add(time.Second)
}
