// Copyright (c) 2026 Adam Ousmer. All rights reserved.
package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/lifecycle"
	"context"
	"go.uber.org/zap"
	"time"
)

func (p *Pipeline) runtimeAdmission(ctx context.Context, broadcasterID uint64) (channelruntime.Snapshot, bool, error) {
	if p.runtime == nil {
		return channelruntime.Snapshot{}, true, nil
	}
	state, err := p.runtime.Get(ctx, broadcasterID)
	return state, state.AllowsRuntime(), err
}

func isStreamLifecycle(eventType string) bool {
	return eventType == "stream.online" || eventType == "stream.offline"
}

func (p *Pipeline) recordActivity(ctx context.Context, mctx *module.Context, emission *emitState, at time.Time) {
	if p.runtime == nil || isStreamLifecycle(mctx.Env.Type) {
		return
	}
	if !mctx.Engaged && emission.ordinal == 0 {
		return
	}
	activity := channelruntime.Activity{EventID: mctx.Env.EventID, At: at}
	if _, err := p.runtime.Engage(ctx, mctx.BroadcasterID, emission.admission, activity); err != nil {
		p.log.Warn("lifecycle activity update failed", module.BIDField(mctx.BroadcasterID), zap.Error(err))
	}
}

func (s *emitState) outputCanWake() bool {
	if !s.admission.Registered || s.env == nil {
		return false
	}
	if s.admission.State.RuntimeState != lifecycle.Dormant {
		return false
	}
	return !isStreamLifecycle(s.env.Type)
}

func (p *Pipeline) wakeForOutput(ctx context.Context, state *emitState) (bool, error) {
	if p.runtime == nil || !state.outputCanWake() {
		return true, nil
	}
	activity := channelruntime.Activity{EventID: state.env.EventID, At: time.Now()}
	next, err := p.runtime.Engage(ctx, state.mctx.BroadcasterID, state.admission, activity)
	if err != nil {
		return false, err
	}
	if !next.AllowsRuntime() {
		return false, nil
	}
	state.admission = next
	return true, nil
}
