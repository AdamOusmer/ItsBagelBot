// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"go.uber.org/zap"
)

type emitState struct {
	admission  channelruntime.Snapshot
	subject    string
	replayBase string
	locale     string
	mctx       *module.Context
	env        *lane.Envelope
	baseDone   bool
	ordinal    int
	accepted   int
	needsFlush bool
	err        error
	legacy     *idempotency.Handoff
}

func (s *emitState) replayID() string {
	if !s.baseDone {
		s.replayBase = outputReplayBase(s.env)
		s.baseDone = true
	}
	if s.replayBase == "" {
		return ""
	}
	if s.mctx != nil && s.mctx.EffectScope != "" {
		s.mctx.EffectOrdinal++
		return replayOutputID(s.replayBase+":"+s.mctx.EffectScope, s.mctx.EffectOrdinal)
	}
	return replayOutputID(s.replayBase, s.ordinal)
}

func (s *emitState) countOutput() {
	s.ordinal++
}

func (s *emitState) acceptOutput(o *module.Output) {
	n := outputCount(o)
	s.accepted += n
	if s.mctx != nil {
		s.mctx.Emitted += n
	}
}

func outputCount(o *module.Output) int {
	if o.Type != outgress.TypeBatch {
		return 1
	}
	n := 0
	for i := range o.Items {
		n += outputCount(&o.Items[i])
	}
	return n
}

func (p *Pipeline) newEmit(ctx context.Context, partition string, state *emitState) module.Emit {
	ctx = bus.WithPublishPartition(ctx, partition)
	return func(o *module.Output) {
		if state.err != nil {
			return
		}
		if !p.prepareEmission(ctx, state, o) {
			return
		}
		state.countOutput()
		replayID := state.replayID()
		accepted, err := p.publishOutput(ctx, state, replayID, o)
		if err != nil {
			state.err = err
			state.fail(ctx, err)
			return
		}
		if !accepted {
			return
		}
		state.acceptOutput(o)
		if replayID == "" {
			state.needsFlush = true
			state.joinLegacy(ctx)
		}
	}
}

func (p *Pipeline) prepareEmission(ctx context.Context, state *emitState, o *module.Output) bool {
	if o == nil {
		return false
	}
	if o.Type == "" {
		return false
	}
	Translate(o)
	applyOutputLocale(o, state.outputLocale(ctx))
	if isEmptyAction(o) {
		return false
	}
	capEmitText(o)
	return !p.floorSuppressed(o)
}

func (s *emitState) joinLegacy(ctx context.Context) {
	if s.mctx != nil && s.mctx.EffectContext != nil {
		ctx = s.mctx.EffectContext
	}
	if !idempotency.Tracking(ctx) {
		return
	}
	if s.legacy == nil {
		s.legacy = &idempotency.Handoff{}
	}
	idempotency.Join(ctx, s.legacy)
}

func (s *emitState) finishLegacy() {
	if s.legacy != nil {
		s.legacy.Resolve(s.err)
	}
}

func (s *emitState) fail(ctx context.Context, err error) {
	idempotency.Fail(ctx, err)
	if s.mctx == nil {
		return
	}
	if s.mctx.EffectContext != nil {
		idempotency.Fail(s.mctx.EffectContext, err)
	}
}

func applyOutputLocale(o *module.Output, locale string) {
	o.Locale = locale
	for i := range o.Items {
		applyOutputLocale(&o.Items[i], locale)
	}
}

func (p *Pipeline) laneSubject(regress module.Regress) string {
	if regress.IsPremium() {
		return p.outgressPremium
	}
	return p.outgressStandard
}

func (p *Pipeline) floorSuppressed(o *module.Output) bool {
	if o.Text == "" {
		return false
	}
	switch o.Type {
	case outgress.TypeChat, outgress.TypeAnnounce, outgress.TypePin:
	default:
		return false
	}
	term, hit := moderation.CheckFloor(o.Text)
	if hit {
		p.log.Warn("suppressed outgoing message carrying floor content",
			zap.String("term", term),
			zap.String("broadcaster_id", o.BroadcasterID))
	}
	return hit
}

func (p *Pipeline) publishOutput(ctx context.Context, state *emitState, replayID string, o *module.Output) (bool, error) {
	allowed, err := p.wakeForOutput(ctx, state)
	if err != nil || !allowed {
		return false, err
	}
	body, err := p.encodeOutput(ctx, state, o)
	if err != nil {
		return false, err
	}
	err = publishWithin(ctx, p.pub, bus.Publication{Subject: state.subject, ID: replayID, Payload: body})
	return err == nil, err
}

func (p *Pipeline) encodeOutput(ctx context.Context, state *emitState, o *module.Output) ([]byte, error) {
	encodeSegment := startStage(ctx, "sesame.output.encode")
	output, err := buildOutgressMessage(o)
	if err != nil {
		endStage(encodeSegment, "error")
		return nil, err
	}
	if state.env != nil && state.env.Origin == "trial" {
		p.addTrial(ctx, state.env.BroadcasterUserID, "blocked", int64(markTrialOutput(&output, state.env.TrialGeneration)))
	}
	if state.admission.Registered {
		output.AccountCreatedAt = state.admission.AccountCreatedAt
		output.AdmissionGeneration = state.admission.State.AdmissionGeneration
	}
	output.EventAt = outputEventTime(state.env)
	body, err := codec.Marshal(&output)
	if err != nil {
		endStage(encodeSegment, "error")
		return nil, err
	}
	endStage(encodeSegment, "ok")
	return body, nil
}

func outputEventTime(env *lane.Envelope) int64 {
	if env == nil {
		return 0
	}
	return env.EventVersion()
}

func outputReplayBase(env *lane.Envelope) string {
	if env == nil || env.EventID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(env.BroadcasterUserID + "\x00" + env.EventID))
	return "sesame:" + hex.EncodeToString(sum[:16])
}

func replayOutputID(base string, ordinal int) string {
	if base == "" {
		return ""
	}
	return base + ":" + strconv.Itoa(ordinal)
}
