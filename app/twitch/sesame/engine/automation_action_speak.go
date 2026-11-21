// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
)

func (x *actionExec) speak(s step, p automation.SpeakAction) error {
	if !x.p.speech.Premium(x.ctx, x.run.BroadcasterID) {
		return limited("premium_required")
	}
	text := voiceplan.TrimRunes(voiceplan.PlainText(x.expand(p.Text)), voiceplan.MaxSpeechRunes)
	if !voiceplan.HasSpokenContent(text) || x.blocksSpeech(text) {
		return skipped("speech_blocked")
	}
	allowed, err := x.p.speech.Allowed(x.ctx, x.run.BroadcasterID)
	if err != nil {
		return err
	}
	if !allowed {
		return limited("voice_unavailable")
	}
	return x.p.speech.Publish(x.ctx, SpeechRequest{
		Kind: voiceplan.SpeechRecipe, BroadcasterID: x.run.BroadcasterID,
		Identity: x.run.replayKey() + ":" + s.EffectKey + x.iter, TraceID: bus.OriginTraceID(x.ctx),
		Text: voiceplan.StripControls(text), Style: voiceplan.Style(p.Style), Voice: p.Voice,
	})
}

func (x *actionExec) blocksSpeech(text string) bool {
	if c := x.run.Ctx; c != nil && c.BlocksText != nil {
		return c.BlocksText(text)
	}
	return false
}
