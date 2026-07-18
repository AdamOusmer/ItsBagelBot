package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

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
type IDFunc func(req gatewayrpc.Request) (id ID, reject string)

// Account is the default IDFunc: the trimmed Request.Account, cache-keyed
// case-insensitively.
func Account(req gatewayrpc.Request) (ID, string) {
	a := strings.TrimSpace(req.Account)
	if a == "" {
		return ID{}, "missing account"
	}
	return ID{Display: a, Key: strings.ToLower(a)}, ""
}

// Channel identifies the flow by the trimmed Request.ChannelID.
func Channel(req gatewayrpc.Request) (ID, string) {
	c := strings.TrimSpace(req.ChannelID)
	if c == "" {
		return ID{}, "missing channel"
	}
	return ID{Display: c, Key: c}, ""
}

// StaticID identifies every request identically, for endpoints whose reply
// carries no request state (the fortnite item shop).
func StaticID(key string) IDFunc {
	return func(gatewayrpc.Request) (ID, string) { return ID{Key: key}, "" }
}

// FetchFunc produces one endpoint's typed success reply for a validated
// identity. Upstream failures return typed errors (*core.UpstreamError) so the
// flow maps them onto friendly reply errors; anything else propagates as an
// infrastructure failure.
type FetchFunc func(ctx context.Context, req gatewayrpc.Request, id ID) (any, error)

// ReplyFunc shapes the endpoint's typed reply-with-Error for one identity and
// message. The flow uses it for every failure it answers: a rejected identity,
// a friendly upstream failure, and the infrastructure fallback.
type ReplyFunc func(id, msg string) any

// flowSpec is one declared byte-flow endpoint: the caching windows plus the
// identity, reply shaping and fetch the FlowBuilder chained.
type flowSpec struct {
	ttl         time.Duration
	negativeTTL time.Duration
	id          IDFunc
	reply       ReplyFunc
	fallback    string
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
	case f.ttl <= 0:
		return fmt.Errorf("endpoint %q flow has a non-positive TTL", ref.endpoint)
	case d.Cache == nil:
		return fmt.Errorf("endpoint %q is cached but Deps.Cache is nil", ref.endpoint)
	}
	return nil
}

// handler assembles the endpoint's HandlerFunc. A hit answers the stored wire
// bytes untouched (json.RawMessage passes through the engine verbatim); a miss
// runs fetch through core.BuildReply so successes and friendly failures are
// shaped and marshaled exactly once.
func (f *flowSpec) handler(d Deps, ref endpointRef) HandlerFunc {
	cache, log := d.Cache, d.Log
	fallback := f.fallback
	if fallback == "" {
		fallback = "lookup failed"
	}
	return func(ctx context.Context, req gatewayrpc.Request) any {
		id, reject := f.id(req)
		if reject != "" {
			return f.reply(id.Display, reject)
		}
		b, err := core.CachedBytes(ctx, cache, core.Key(ref.provider, ref.endpoint, id.Key),
			func(ctx context.Context) ([]byte, time.Duration, error) {
				return core.BuildReply(ctx, f.ttl, f.negativeTTL,
					func(ctx context.Context) (any, error) { return f.fetch(ctx, req, id) },
					func(msg string) any { return f.reply(id.Display, msg) },
				)
			})
		if err != nil {
			log.Warn("gateway fetch failed",
				zap.String("provider", ref.provider),
				zap.String("endpoint", ref.endpoint),
				zap.String("id", id.Display),
				zap.Error(err))
			return f.reply(id.Display, fallback)
		}
		return json.RawMessage(b)
	}
}
