// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/idempotency"
	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type boundHandler struct {
	m       module.Module
	handle  module.EventHandler
	ordinal int
}

func (p *Pipeline) dispatch(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) {
	mctx.EffectScope, mctx.EffectOrdinal = "command", 0
	err := p.dispatchCommand(ctx, mctx, views, emit)
	if err != nil {
		p.log.Error("command dispatch failed", module.BIDField(mctx.BroadcasterID), zap.Error(err))
		notice(ctx, err)
		idempotency.Fail(ctx, err)
		mctx.Failure = err
	}
}

func (p *Pipeline) runHandlers(ctx context.Context, views map[string]projection.ModuleView, mctx *module.Context, emit module.Emit) {
	ctx = p.prepareHandlerEffects(ctx, mctx, views)
	eventType := mctx.Env.Type
	switch eventType {
	case "channel.cheer", "channel.subscription.message":
		mctx.BlocksText = automodConfigFrom(views, false).BlocksText
	}
	for ordinal, m := range p.registry.For(eventType) {
		if !p.enabled(m, views, mctx) {
			continue
		}
		h := boundHandler{m: m, handle: m.Events[eventType], ordinal: ordinal}
		p.runHandler(ctx, mctx, &h, emit)
	}
}

func (p *Pipeline) runHandler(ctx context.Context, mctx *module.Context, h *boundHandler, emit module.Emit) {
	if h.handle == nil {
		return
	}
	before := mctx.Emitted
	mctx.EffectScope, mctx.EffectOrdinal = "", 0
	if mctx.Env.EventID != "" || p.dedup != nil {
		mctx.EffectScope = h.effectScope()
	}
	err := p.runHandlerEffect(ctx, mctx, h, emit)
	if err != nil {
		p.handlerFailed(ctx, mctx, h.m, err)
		idempotency.Fail(ctx, err)
		mctx.Failure = err
		return
	}
	if engagedBy(mctx, h.m, before) {
		mctx.Engaged = true
	}
}

func (h *boundHandler) effectScope() string {
	if h.m.Name != "" {
		return "handler:" + h.m.Name
	}
	return "handler:core:" + strconv.Itoa(h.ordinal)
}

func (p *Pipeline) runHandlerEffect(ctx context.Context, c *module.Context, h *boundHandler, emit module.Emit) error {
	if h.m.ProtectReplay {
		return p.dedup.Effect(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: c.EffectScope}, func(ctx context.Context) error {
			return p.invokeHandler(ctx, c, h, emit)
		})
	}
	return p.invokeHandler(ctx, c, h, emit)
}

func (p *Pipeline) invokeHandler(ctx context.Context, c *module.Context, h *boundHandler, emit module.Emit) error {
	c.EffectContext = ctx
	before := c.Emitted
	err := h.handle(ctx, c, emit)
	if err == nil {
		p.recordHandled(ctx, c, h.m, before)
	}
	return err
}

func engagedBy(mctx *module.Context, m module.Module, emittedBefore int) bool {
	if !viewerEngagement(mctx.Env.Type) {
		return false
	}
	return mctx.Emitted > emittedBefore || m.Kind == module.KindOptIn
}

func viewerEngagement(eventType string) bool {
	if eventType == chatType || strings.HasPrefix(eventType, "channel.ad_break.") {
		return false
	}
	return strings.HasPrefix(eventType, "channel.")
}

func (p *Pipeline) handlerFailed(ctx context.Context, mctx *module.Context, m module.Module, err error) {
	p.log.Error("module handler failed",
		zap.String("module", moduleLabel(m)),
		zap.String("type", mctx.Env.Type),
		module.BIDField(mctx.BroadcasterID),
		zap.Error(err))
	if txn := newrelic.FromContext(ctx); txn != nil {
		txn.AddAttribute("module.failed", moduleLabel(m))
		monitor.NoticeError(txn, err)
	}
}

func notice(ctx context.Context, err error) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		monitor.NoticeError(txn, err)
	}
}

func (p *Pipeline) enabled(m module.Module, views map[string]projection.ModuleView, mctx *module.Context) bool {
	if m.Trial {
		mctx.Config = nil
		return mctx.Env.Origin == "trial"
	}
	if m.Beta && !mctx.Regress.IsPremium() {
		return false
	}
	switch m.Kind {
	case module.KindCore:
		mctx.Config = nil
		return true
	case module.KindDefault:
		mv, ok := views[m.Name]
		return enabledByView(mv, ok, mctx, module.KindDefault)
	case module.KindOptIn:
		mv, ok := views[m.Name]
		return enabledByView(mv, ok, mctx, module.KindOptIn)
	default:
		return false
	}
}

func enabledByView(mv projection.ModuleView, ok bool, mctx *module.Context, kind module.Kind) bool {
	optIn := kind == module.KindOptIn
	if optIn && mctx.Env.Origin == "trial" {
		return false
	}
	if !ok {
		if !optIn {
			mctx.Config = nil
		}
		return !optIn
	}
	if !mv.IsEnabled {
		return false
	}
	mctx.Config = mv.Configs
	return true
}
