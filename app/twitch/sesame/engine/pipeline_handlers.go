// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type boundHandler struct {
	m      module.Module
	handle module.EventHandler
}

func (p *Pipeline) dispatch(ctx context.Context, mctx *module.Context, views map[string]projection.ModuleView, emit module.Emit) {
	if err := p.dispatchCommand(ctx, mctx, views, emit); err != nil {
		p.log.Error("command dispatch failed", module.BIDField(mctx.BroadcasterID), zap.Error(err))
		notice(ctx, err)
	}
}

func (p *Pipeline) runHandlers(ctx context.Context, views map[string]projection.ModuleView, mctx *module.Context, emit module.Emit, eventType string) {
	for _, m := range p.registry.For(eventType) {
		if !p.enabled(m, views, mctx) {
			continue
		}
		h := boundHandler{m: m, handle: m.Events[eventType]}
		p.runHandler(ctx, mctx, &h, emit)
	}
}

func (p *Pipeline) runHandler(ctx context.Context, mctx *module.Context, h *boundHandler, emit module.Emit) {
	if h.handle == nil {
		return
	}
	before := mctx.Emitted
	started := time.Now()
	if err := h.handle(ctx, mctx, emit); err != nil {
		p.handlerFailed(ctx, mctx, h.m, err)
		return
	}
	p.recordHandled(mctx, h.m, before, time.Since(started))
	if engagedBy(mctx, h.m, before) {
		mctx.Engaged = true
	}
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
	p.recordFailure(m, err)
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
