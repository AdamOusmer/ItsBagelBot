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
	GetChattersPage(context.Context, twitch.ChattersPageRequest) (twitch.ChattersPage, error)
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

// A request either asks for one correlated watch page or the legacy complete
// viewer snapshot. These protocols share HTTP admission, never award ownership.
func isViewerRequest(req manage.ChattersRequest) bool {
	for _, value := range []string{req.RequestID, req.WindowID, req.SessionGeneration, req.LiveSession, req.Cursor} {
		if value != "" {
			return false
		}
	}
	return !req.CheckLive
}

func correlatedReply(req manage.ChattersRequest) manage.ChattersReply {
	return manage.ChattersReply{BroadcasterID: req.BroadcasterID, RequestID: req.RequestID, WindowID: req.WindowID, SessionGeneration: req.SessionGeneration, LiveSession: req.LiveSession}
}

func (c *chatters) handleGet(parent context.Context, req manage.ChattersRequest) manage.ChattersReply {
	ctx, cancel, err := c.requestContext(parent, req)
	if err != nil {
		return c.failedReply(req, correlatedReply(req), err)
	}
	defer cancel()
	if isViewerRequest(req) {
		return c.collectResponse(ctx, req, c.collectViewerReply)
	}
	return c.collectResponse(ctx, req, c.collectWatchPage)
}
func (c *chatters) requestContext(parent context.Context, req manage.ChattersRequest) (context.Context, context.CancelFunc, error) {
	if isViewerRequest(req) {
		return c.viewerContext(parent, req)
	}
	return c.watchContext(parent, req)
}
func (c *chatters) collectResponse(ctx context.Context, req manage.ChattersRequest, collect func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error)) manage.ChattersReply {
	if err := c.preflight(ctx, req); err != nil {
		return c.failedReply(req, correlatedReply(req), err)
	}
	reply, err := collect(c.attemptContext(ctx, req), req)
	if err != nil {
		return c.failedReply(req, reply, err)
	}
	return reply
}

func validBroadcaster(id string) bool {
	if !boundedIdentity(id, 64) {
		return false
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return false
	}
	return n != 0
}
func boundedIdentity(value string, limit int) bool {
	if value == "" {
		return false
	}
	return len(value) <= limit
}
func validWatchRequest(req manage.ChattersRequest) bool {
	if !validBroadcaster(req.BroadcasterID) {
		return false
	}
	if !boundedIdentity(req.RequestID, 128) {
		return false
	}
	if !boundedIdentity(req.WindowID, 128) {
		return false
	}
	if len(req.Cursor) > 4096 {
		return false
	}
	return req.DeadlineUnixMilli > 0
}
func (c *chatters) watchContext(parent context.Context, req manage.ChattersRequest) (context.Context, context.CancelFunc, error) {
	if c.botID == "" {
		return nil, nil, &twitch.AdmissionError{Code: "invalid"}
	}
	if !validWatchRequest(req) {
		return nil, nil, &twitch.AdmissionError{Code: "invalid"}
	}
	deadline := time.UnixMilli(req.DeadlineUnixMilli)
	if !deadline.After(c.now()) {
		return nil, nil, &twitch.AdmissionError{Code: "expired"}
	}
	deadline = minDeadline(deadline, c.now().Add(chattersHandleTimeout))
	ctx, cancel := context.WithDeadline(parent, deadline)
	return ctx, cancel, nil
}
func minDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (c *chatters) preflight(ctx context.Context, req manage.ChattersRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.opts.Limiter == nil {
		return c.unavailable()
	}
	active, err := c.admitTenant(ctx, req)
	if err != nil {
		return err
	}
	if !active {
		return &twitch.AdmissionError{Code: "inactive"}
	}
	return nil
}
func (c *chatters) attemptContext(ctx context.Context, req manage.ChattersRequest) context.Context {
	ctx = twitch.WithAttemptAdmission(ctx, c.admit(req))
	return twitch.WithProviderResetObserver(ctx, func(ctx context.Context, endpoint string, reset time.Time) error {
		return c.recordProviderReset(ctx, c.providerIdentity(endpoint), reset)
	})
}
func (c *chatters) failedReply(req manage.ChattersRequest, reply manage.ChattersReply, err error) manage.ChattersReply {
	code, retryAt := chattersFailure(err, c.now())
	reply.ErrorCode = code
	reply.Error = "attendance request failed"
	reply.MissingScope = code == "authorization"
	if !retryAt.IsZero() {
		reply.RetryAtUnixMilli = retryAt.UnixMilli()
	}
	c.logQuotaRejection(req, reply)
	return reply
}
func (c *chatters) logQuotaRejection(req manage.ChattersRequest, reply manage.ChattersReply) {
	if reply.ErrorCode != "rate_limited" {
		return
	}
	if c.log == nil {
		return
	}
	c.log.Debug("watchtime: attendance quota rejected", zap.String("broadcaster_id", req.BroadcasterID), zap.String("request_id", req.RequestID), zap.String("window_id", req.WindowID), zap.Bool("check_live", req.CheckLive), zap.Int64("retry_at_unix_milli", reply.RetryAtUnixMilli))
}
func (c *chatters) collectWatchPage(ctx context.Context, req manage.ChattersRequest) (manage.ChattersReply, error) {
	reply := correlatedReply(req)
	if req.CheckLive {
		var err error
		reply, err = c.confirmLive(ctx, req, reply)
		if err != nil {
			return reply, err
		}
		if !reply.Live {
			return reply, nil
		}
	}
	page, err := c.fetchChatterPage(ctx, req.BroadcasterID, req.Cursor)
	if err != nil {
		return reply, err
	}
	reply.Chatters = appendChatters(nil, page.Chatters)
	reply.NextCursor = page.NextCursor
	reply.Complete = page.Complete
	return reply, nil
}
func (c *chatters) confirmLive(ctx context.Context, req manage.ChattersRequest, reply manage.ChattersReply) (manage.ChattersReply, error) {
	streamID, startedAt, live, err := c.twitch.StreamSession(ctx, req.BroadcasterID)
	reply.StreamID = streamID
	if !startedAt.IsZero() {
		reply.StreamStartedAtUnixMilli = startedAt.UnixMilli()
	}
	if err != nil {
		return reply, c.providerError(ctx, "helix:app", err)
	}
	reply.Live = live
	reply.CheckedAtUnixMilli = c.now().UnixMilli()
	reply.Complete = !live
	return reply, nil
}
func (c *chatters) fetchChatterPage(ctx context.Context, broadcasterID, cursor string) (twitch.ChattersPage, error) {
	page, err := c.twitch.GetChattersPage(ctx, twitch.ChattersPageRequest{BroadcasterID: broadcasterID, ModeratorID: c.botID, Cursor: cursor})
	if err != nil {
		return page, c.providerError(ctx, "helix:bot:"+c.botID, err)
	}
	return page, nil
}
func appendChatters(out []manage.Chatter, chatters []twitch.Chatter) []manage.Chatter {
	for _, ch := range chatters {
		out = append(out, manage.Chatter{ID: ch.ID, Login: ch.Login})
	}
	return out
}

// The legacy viewer cache accepts only complete bounded listings. Failed pages
// never expose their partially collected attendance as a successful snapshot.
func (c *chatters) collectViewerReply(ctx context.Context, req manage.ChattersRequest) (manage.ChattersReply, error) {
	reply := correlatedReply(req)
	entries, err := c.collectViewer(ctx, req.BroadcasterID)
	if err != nil {
		return reply, err
	}
	reply.Chatters = entries
	reply.Complete = true
	return reply, nil
}
func (c *chatters) viewerContext(parent context.Context, req manage.ChattersRequest) (context.Context, context.CancelFunc, error) {
	if !validBroadcaster(req.BroadcasterID) {
		return nil, nil, &twitch.AdmissionError{Code: "invalid"}
	}
	if c.botID == "" {
		return nil, nil, &twitch.AdmissionError{Code: "invalid"}
	}
	deadline := c.now().Add(viewerChattersTimeout)
	if req.DeadlineUnixMilli > 0 {
		deadline = minDeadline(deadline, time.UnixMilli(req.DeadlineUnixMilli))
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	return ctx, cancel, nil
}

type viewerPagination struct {
	cursor string
	seen   map[string]bool
}

func (p *viewerPagination) advance(next string) error {
	if next == "" {
		return twitch.ErrRepeatedCursor
	}
	if next == p.cursor {
		return twitch.ErrRepeatedCursor
	}
	if p.seen[next] {
		return twitch.ErrRepeatedCursor
	}
	p.seen[next] = true
	p.cursor = next
	return nil
}
func (c *chatters) collectViewer(ctx context.Context, id string) ([]manage.Chatter, error) {
	progress := viewerPagination{seen: map[string]bool{}}
	var entries []manage.Chatter
	for range viewerChattersMaxPages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := c.fetchChatterPage(ctx, id, progress.cursor)
		if err != nil {
			return nil, err
		}
		entries = appendChatters(entries, page.Chatters)
		if page.Complete {
			return entries, nil
		}
		if err := progress.advance(page.NextCursor); err != nil {
			return nil, err
		}
	}
	return nil, twitch.ErrChattersIncomplete
}

func (c *chatters) admitTenant(ctx context.Context, req manage.ChattersRequest) (bool, error) {
	if req.RequestID == "" {
		return c.admitViewer(ctx, req.BroadcasterID)
	}
	if c.opts.Admit == nil {
		return false, errors.New("watch admission unavailable")
	}
	return c.opts.Admit(ctx, req)
}
func (c *chatters) admitViewer(ctx context.Context, id string) (bool, error) {
	if c.opts.AdmitViewer == nil {
		return false, errors.New("viewer admission unavailable")
	}
	return c.opts.AdmitViewer(ctx, id)
}
func (c *chatters) tenantReady(ctx context.Context, req manage.ChattersRequest) error {
	active, err := c.admitTenant(ctx, req)
	if err != nil {
		return c.unavailable()
	}
	if !active {
		return &twitch.AdmissionError{Code: "inactive"}
	}
	return nil
}
func (c *chatters) unavailable() error {
	return &twitch.AdmissionError{Code: "unavailable", RetryAt: c.now().Add(time.Second)}
}
func (c *chatters) admit(req manage.ChattersRequest) twitch.AttemptAdmission {
	return func(ctx context.Context, endpoint string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.tenantReady(ctx, req); err != nil {
			return err
		}
		if err := c.providerReady(ctx, endpoint); err != nil {
			return err
		}
		if err := c.payTenant(ctx, req.BroadcasterID); err != nil {
			return err
		}
		return c.payShared(ctx, endpoint)
	}
}
func (c *chatters) providerReady(ctx context.Context, endpoint string) error {
	if c.opts.ProviderRetryAt == nil {
		return nil
	}
	reset, err := c.opts.ProviderRetryAt(ctx, c.providerIdentity(endpoint))
	if err != nil {
		return c.unavailable()
	}
	// Server TIME decides whether a cooldown is live, never this pod's clock.
	if !reset.IsZero() {
		return &twitch.AdmissionError{Code: "rate_limited", RetryAt: reset}
	}
	return nil
}
func (c *chatters) payTenant(ctx context.Context, id string) error {
	tenant := chatterTenantSpec.ForDynamicKey("ratelimit:watch:tenant:", "watch:tenant", id)
	allowed, err := c.opts.Limiter.Allow(ctx, tenant)
	if err != nil {
		return c.unavailable()
	}
	if !allowed {
		return &twitch.AdmissionError{Code: "rate_limited", RetryAt: c.now().Add(2 * time.Second)}
	}
	return nil
}
func attendanceQuotas(endpoint string) (ratelimit.Request, ratelimit.Request) {
	if strings.HasPrefix(endpoint, "/helix/streams?") {
		return chatterLiveSpec.ForKey("ratelimit:watch:live"), ratelimit.HelixAppRequest()
	}
	return chatterWatchSpec.ForKey("ratelimit:watch:chatters"), ratelimit.HelixBotRequest()
}
func (c *chatters) payShared(ctx context.Context, endpoint string) error {
	share, shared := attendanceQuotas(endpoint)
	denied, err := c.opts.Limiter.AllowOrdered(ctx, share, shared)
	if err != nil {
		return c.unavailable()
	}
	if denied != 0 {
		return &twitch.AdmissionError{Code: "rate_limited", RetryAt: c.now().Add(time.Second)}
	}
	return nil
}

func (c *chatters) providerError(ctx context.Context, identity string, err error) error {
	code, retryAt := c.providerFailure(ctx, identity, err)
	return &twitch.AdmissionError{Code: code, RetryAt: retryAt}
}

// Only actual provider feedback establishes token-wide cooldown; quota denials
// and canceled work cannot masquerade as a provider observation.
func (c *chatters) providerFailure(ctx context.Context, identity string, err error) (string, time.Time) {
	if persistErr := c.persistUnobservedReset(ctx, identity, err); persistErr != nil {
		return "unavailable", c.now().Add(time.Second)
	}
	return chattersFailure(err, c.now())
}
func (c *chatters) persistUnobservedReset(ctx context.Context, identity string, err error) error {
	var observed *twitch.AdmissionError
	if !errors.As(err, &observed) {
		return nil
	}
	if !observed.Provider {
		return nil
	}
	if observed.ProviderObserved {
		return nil
	}
	return c.recordProviderReset(ctx, identity, observed.RetryAt)
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
		return c.unavailable()
	}
	return nil
}
func chattersFailure(err error, now time.Time) (string, time.Time) {
	var admission *twitch.AdmissionError
	if errors.As(err, &admission) {
		return admission.Code, admission.RetryAt
	}
	if matchesError(err, context.DeadlineExceeded, context.Canceled) {
		return "expired", time.Time{}
	}
	if errors.Is(err, twitch.ErrRepeatedCursor) {
		return "repeated_cursor", time.Time{}
	}
	if authorizationFailure(err) {
		return "authorization", time.Time{}
	}
	return chatterStatusFailure(err, now)
}
func matchesError(err error, candidates ...error) bool {
	for _, candidate := range candidates {
		if errors.Is(err, candidate) {
			return true
		}
	}
	return false
}
func authorizationFailure(err error) bool {
	if matchesError(err, twitch.ErrMissingScope, twitch.ErrNoUserToken) {
		return true
	}
	return twitch.GrantDead(err)
}
func chatterStatusFailure(err error, now time.Time) (string, time.Time) {
	var status *twitch.StatusError
	if !errors.As(err, &status) {
		return "unavailable", now.Add(time.Second)
	}
	switch status.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authorization", time.Time{}
	case http.StatusBadRequest:
		return "invalid", time.Time{}
	case http.StatusTooManyRequests:
		return "rate_limited", now.Add(time.Second)
	default:
		return "unavailable", now.Add(time.Second)
	}
}
