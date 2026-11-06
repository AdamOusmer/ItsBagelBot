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
	needsFlush bool
	err        error
}

func (s *emitState) replayID() string {
	if !s.baseDone {
		s.replayBase = outputReplayBase(s.env)
		s.baseDone = true
	}
	return replayOutputID(s.replayBase, s.ordinal)
}

func (s *emitState) countOutput() {
	s.ordinal++
	if s.mctx != nil {
		s.mctx.Emitted++
	}
}

func (p *Pipeline) newEmit(ctx context.Context, partition string, state *emitState) module.Emit {
	ctx = bus.WithPublishPartition(ctx, partition)
	return func(o *module.Output) {
		if state.err != nil {
			return
		}
		if o == nil {
			return
		}
		if o.Type == "" {
			return
		}
		Translate(o)
		applyOutputLocale(o, state.outputLocale(ctx))
		if isEmptyAction(o) {
			return
		}
		capEmitText(o)
		if p.floorSuppressed(o) {
			return
		}
		state.countOutput()
		replayID := state.replayID()
		if err := p.publishOutput(ctx, state, replayID, o); err != nil {
			state.err = err
			return
		}
		if replayID == "" {
			state.needsFlush = true
		}
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

func (p *Pipeline) publishOutput(ctx context.Context, state *emitState, replayID string, o *module.Output) error {
	allowed, err := p.wakeForOutput(ctx, state)
	if err != nil || !allowed {
		return err
	}
	body, err := p.encodeOutput(ctx, state, o)
	if err != nil {
		return err
	}
	return publishWithin(ctx, p.pub, bus.Publication{Subject: state.subject, ID: replayID, Payload: body})
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
	body, err := codec.Marshal(&output)
	if err != nil {
		endStage(encodeSegment, "error")
		return nil, err
	}
	endStage(encodeSegment, "ok")
	return body, nil
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
