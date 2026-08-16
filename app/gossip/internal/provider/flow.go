// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// ID is one validated request identity a flow feeds its Fetch and Reply
// shaper. Display is echoed in replies exactly as the caller typed it; Key
// discriminates cache entries (normalized, so "Player" and "player" share one
// entry).
type ID struct {
	Display string
	Key     string
}

// IDFunc extracts and validates a flow's identity from the request. A
// non-empty reject message answers immediately with the endpoint's error reply
// (no cache read, no upstream call); Display may still be set so the reply
// echoes the input.
type IDFunc func(req gossiprpc.Request) (id ID, reject string)

// Account is the default IDFunc: the trimmed Request.Account, cache-keyed
// case-insensitively.
func Account(req gossiprpc.Request) (ID, string) {
	a := strings.TrimSpace(req.Account)
	if a == "" {
		return ID{}, "missing account"
	}
	return ID{Display: a, Key: strings.ToLower(a)}, ""
}

// Channel identifies the flow by the trimmed Request.ChannelID.
func Channel(req gossiprpc.Request) (ID, string) {
	c := strings.TrimSpace(req.ChannelID)
	if c == "" {
		return ID{}, "missing channel"
	}
	return ID{Display: c, Key: c}, ""
}

// StaticID identifies every request identically, for endpoints whose reply
// carries no request state (the fortnite item shop).
func StaticID(key string) IDFunc {
	return func(gossiprpc.Request) (ID, string) { return ID{Key: key}, "" }
}

// FetchFunc produces one endpoint's typed success reply for a validated
// identity. Upstream failures return typed errors (*core.UpstreamError) so the
// flow maps them onto friendly reply errors; anything else propagates as an
// infrastructure failure.
type FetchFunc func(ctx context.Context, req gossiprpc.Request, id ID) (any, error)

// ReplyFunc shapes the endpoint's typed reply-with-Error for one identity and
// message. The flow uses it for every failure it answers: a rejected identity,
// a friendly upstream failure, and the infrastructure fallback.
type ReplyFunc func(id, msg string) any

// AdmitFunc spends one request's share of an endpoint's upstream budget,
// returning a typed *core.UpstreamError (a 429 with LocalDeny set) when this
// request may not spend it. It is the endpoint's premium/standard lane decision,
// so it MUST read the lane from the request it is given rather than from any
// request captured earlier.
//
// It belongs here, declared alongside the fetch, rather than inside the fetch
// itself. A fetch runs once per singleflight flight; an AdmitFunc runs once per
// caller. Budget checks written inside a fetch are therefore charged to whichever
// caller happened to win the flight, and its verdict is served to everyone joined
// to it — which is how a drained standard bucket came to deny premium callers the
// reserve they are entitled to.
type AdmitFunc func(ctx context.Context, req gossiprpc.Request) error

// DeadlineFunc reports the instant a cached answer stops being true, for
// content whose freshness is set by an external clock rather than by an
// interval — the Fortnite item shop turning over at 00:00 UTC, not "roughly
// every fifteen minutes". A flow declares one via CachedUntil instead of a
// fixed ttl, and the flow sizes each build's window off the time remaining to
// it (see freshWindow).
//
// It is given the moment the reply was produced rather than reading the clock
// itself, so a test can drive it and so both halves of one build agree on
// "now".
type DeadlineFunc func(now time.Time) time.Time

// flowSpec is one declared byte-flow endpoint: the caching windows plus the
// identity, reply shaping, budget and fetch the FlowBuilder chained. Exactly
// one of ttl and deadline carries the positive window; see freshWindow.
type flowSpec struct {
	ttl         time.Duration
	deadline    DeadlineFunc
	negativeTTL time.Duration
	id          IDFunc
	reply       ReplyFunc
	fallback    string
	admit       AdmitFunc
	fetch       FetchFunc
}

// FlowBuilder chains one byte-flow endpoint, the skeleton every cached
// endpoint used to hand-roll: validate the identity, read through the byte
// cache (stale-while-revalidate), shape friendly upstream failures via the
// Reply shaper, and answer infrastructure failures with the Fallback message
// after logging. Fetch is the terminal that finishes the flow.
type FlowBuilder struct {
	f *flowSpec
}

// ID sets the flow's identity extractor; Account is the default.
func (fb *FlowBuilder) ID(fn IDFunc) *FlowBuilder {
	fb.f.id = fn
	return fb
}

// Reply sets the shaper for every error reply the flow answers. Required.
func (fb *FlowBuilder) Reply(fn ReplyFunc) *FlowBuilder {
	fb.f.reply = fn
	return fb
}

// Fallback sets the reply message for an infrastructure failure (upstream
// unreachable, cache marshal); the default is "lookup failed".
func (fb *FlowBuilder) Fallback(msg string) *FlowBuilder {
	fb.f.fallback = msg
	return fb
}

// Budget sets the flow's per-caller upstream budget check. An endpoint without
// one spends no budget: only endpoints that actually call a metered upstream
// declare it.
func (fb *FlowBuilder) Budget(fn AdmitFunc) *FlowBuilder {
	fb.f.admit = fn
	return fb
}

// Fetch sets the flow's success producer and finishes it. It is terminal: it
// returns nothing so a declaration cannot accidentally continue past it.
func (fb *FlowBuilder) Fetch(fn FetchFunc) {
	fb.f.fetch = fn
}

// endpointRef identifies one declared endpoint (its provider and endpoint
// subject tokens) for cache keys, validation messages and failure logs.
type endpointRef struct {
	provider string
	endpoint string
}

// validate reports the first problem with the declared flow, or nil.
func (f *flowSpec) validate(d Deps, ref endpointRef) error {
	switch {
	case f.reply == nil:
		return fmt.Errorf("endpoint %q flow has no Reply shaper", ref.endpoint)
	case f.fetch == nil:
		return fmt.Errorf("endpoint %q flow has no Fetch (chain .Fetch to finish it)", ref.endpoint)
	case f.ttl <= 0 && f.deadline == nil:
		return fmt.Errorf("endpoint %q flow has a non-positive TTL", ref.endpoint)
	case d.Cache == nil:
		return fmt.Errorf("endpoint %q is cached but Deps.Cache is nil", ref.endpoint)
	}
	return nil
}

// handler assembles the endpoint's HandlerFunc. A hit answers the stored wire
// bytes untouched (codec.RawMessage passes through the engine verbatim); a miss
// runs fetch through core.BuildReply so successes and friendly failures are
// shaped and marshaled exactly once.
func (f *flowSpec) handler(d Deps, ref endpointRef) HandlerFunc {
	cache, log := d.Cache, d.Log
	fallback := f.fallback
	if fallback == "" {
		fallback = "lookup failed"
	}
	return func(ctx context.Context, req gossiprpc.Request) any {
		id, reject := f.id(req)
		if reject != "" {
			return f.reply(id.Display, reject)
		}
		b, err := core.CachedBytes(ctx, cache, core.Key(ref.provider, ref.endpoint, id.Key),
			f.admitter(req),
			func(ctx context.Context) ([]byte, time.Duration, error) {
				b, ttl, friendly, err := core.BuildReply(ctx, f.freshWindow(time.Now()), f.negativeTTL,
					func(ctx context.Context) (any, error) { return f.fetch(ctx, req, id) },
					func(msg string) any { return f.reply(id.Display, msg) },
				)
				logFriendly(log, ref, id, friendly)
				return b, ttl, err
			})
		if err != nil {
			return replier{spec: f, log: log, ref: ref, fallback: fallback}.failure(id, err)
		}
		return codec.RawMessage(b)
	}
}

// freshWindow is the window one build's reply is stored fresh for, measured
// from the moment it was produced.
//
// A fixed-interval flow stores for the ttl it declared. A deadline flow stores
// for HALF the time remaining to its deadline, and the half is the whole point
// rather than a hedge: the byte cache retains an entry for TWICE its fresh
// window (core's storeEntry writes 2*ttl), so halving is exactly what makes the
// entry's physical life end ON the deadline.
//
// Without that, stale-while-revalidate reaches straight past the boundary it
// was given. An entry stored fresh-until-rotation is still physically present
// for a full window afterwards, so the first !store after 00:00 UTC would be
// answered out of it — yesterday's item shop, at the exact moment the shop is
// worth asking about, and at 8pm Eastern that is the middle of a stream. With
// the half, the entire stale tail sits on the correct side of the boundary: a
// read inside it serves content that is still true and re-stores against the
// same deadline (each refresh halves what remains, so it converges on the
// deadline and never crosses it), and the first read after the deadline finds
// nothing at all and fetches the new shop.
//
// A deadline already in the past yields a non-positive window, which
// CachedBytes answers without caching — the correct degradation, since the one
// thing known about that reply is that it is not fresh.
func (f *flowSpec) freshWindow(now time.Time) time.Duration {
	if f.deadline == nil {
		return f.ttl
	}
	return f.deadline(now).Sub(now) / 2
}

// replier is the per-endpoint context every failure reply needs: the flow that
// shapes it, and the logger, endpoint identity and fallback message that are
// fixed once at Build time. They travel together so failure takes only what
// varies per request.
type replier struct {
	spec     *flowSpec
	log      *zap.Logger
	ref      endpointRef
	fallback string
}

// admitter binds one request to the flow's budget check, or returns nil when the
// endpoint declares none. The request is bound per call and never captured across
// calls: the lane it carries is the whole point of the check.
func (f *flowSpec) admitter(req gossiprpc.Request) func(context.Context) error {
	if f.admit == nil {
		return nil
	}
	return func(ctx context.Context) error { return f.admit(ctx, req) }
}

// failure answers one lookup that produced an error rather than reply bytes. A
// budget denial arrives here rather than through BuildReply — it is raised before
// the flight the fetch runs in — so it is shaped through the same friendly mapping
// the fetch path uses, or the caller would be told "lookup failed" for a rate
// limit that has a perfectly good message of its own.
func (r replier) failure(id ID, err error) any {
	if msg, _ := core.FriendlyUpstream(err); msg != "" {
		var friendly *core.UpstreamError
		errors.As(err, &friendly)
		logFriendly(r.log, r.ref, id, friendly)
		return r.spec.reply(id.Display, msg)
	}
	r.log.Warn("gossip fetch failed",
		zap.String("provider", r.ref.provider),
		zap.String("endpoint", r.ref.endpoint),
		zap.String("id", id.Display),
		zap.Error(err))
	return r.spec.reply(id.Display, r.fallback)
}

// logFriendly records the friendly failures that would otherwise be invisible:
// they are shaped into ordinary replies, so without this line neither the
// gossip service nor the caller ever logs that a rate limit or key problem happened —
// exactly the states an operator needs to see. Missing players (400/404) stay
// quiet; they are routine.
func logFriendly(log *zap.Logger, ref endpointRef, id ID, friendly *core.UpstreamError) {
	if friendly == nil {
		return
	}
	switch friendly.Status {
	case 400, 404:
		return
	}
	log.Warn("gossip upstream denial",
		zap.String("provider", ref.provider),
		zap.String("endpoint", ref.endpoint),
		zap.String("id", id.Display),
		zap.Int("status", friendly.Status),
		zap.String("upstream_message", friendly.Message),
		zap.Bool("local_deny", friendly.LocalDeny))
}
