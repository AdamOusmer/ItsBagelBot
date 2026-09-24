// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

type ID struct {
	Display string
	Key     string
}

type IDFunc func(req gossiprpc.Request) (id ID, reject string)

func Account(req gossiprpc.Request) (ID, string) {
	a := strings.TrimSpace(req.Account)
	if a == "" {
		return ID{}, "missing account"
	}
	return ID{Display: a, Key: strings.ToLower(a)}, ""
}

func Channel(req gossiprpc.Request) (ID, string) {
	c := strings.TrimSpace(req.ChannelID)
	if c == "" {
		return ID{}, "missing channel"
	}
	return ID{Display: c, Key: c}, ""
}

func StaticID(key string) IDFunc {
	return func(gossiprpc.Request) (ID, string) { return ID{Key: key}, "" }
}

type FetchFunc func(ctx context.Context, req gossiprpc.Request, id ID) (any, error)

type ReplyFunc func(id, msg string) any

type AdmitFunc func(ctx context.Context, req gossiprpc.Request) error

type DeadlineFunc func(now time.Time) time.Time

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

type FlowBuilder struct {
	f *flowSpec
}

func (fb *FlowBuilder) ID(fn IDFunc) *FlowBuilder {
	fb.f.id = fn
	return fb
}

func (fb *FlowBuilder) Reply(fn ReplyFunc) *FlowBuilder {
	fb.f.reply = fn
	return fb
}

func (fb *FlowBuilder) Fallback(msg string) *FlowBuilder {
	fb.f.fallback = msg
	return fb
}

func (fb *FlowBuilder) Budget(fn AdmitFunc) *FlowBuilder {
	fb.f.admit = fn
	return fb
}

func (fb *FlowBuilder) Fetch(fn FetchFunc) {
	fb.f.fetch = fn
}

type endpointRef struct {
	provider string
	endpoint string
}

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

func (f *flowSpec) freshWindow(now time.Time) time.Duration {
	if f.deadline == nil {
		return f.ttl
	}
	return f.deadline(now).Sub(now) / 2
}

type replier struct {
	spec     *flowSpec
	log      *zap.Logger
	ref      endpointRef
	fallback string
}

func (f *flowSpec) admitter(req gossiprpc.Request) func(context.Context) error {
	if f.admit == nil {
		return nil
	}
	return func(ctx context.Context) error { return f.admit(ctx, req) }
}

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
