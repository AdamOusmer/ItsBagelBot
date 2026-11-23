// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
)

func (x *actionExec) speak(s step, p automation.SpeakAction) error {
	if !x.p.speech.Premium(x.ctx, x.run.BroadcasterID) {
		return limited("premium_required")
	}
	text := voiceplan.TrimRunes(voiceplan.PlainText(x.expand(p.Text)), voiceplan.MaxSpeechRunes)
	if !voiceplan.HasSpokenContent(text) {
		return skipped("speech_blocked")
	}
	req := SpeechRequest{
		Kind: voiceplan.SpeechRecipe, BroadcasterID: x.run.BroadcasterID, Source: voiceplan.SourceChannel,
		Identity: x.run.replayKey() + ":" + s.EffectKey + x.iter, TraceID: bus.OriginTraceID(x.ctx),
		Style: voiceplan.Style(p.Style), Voice: p.Voice,
	}
	text, ok, err := x.p.speech.Screen(x.ctx, req.Subject(), moderation.SpeechInput{Message: text}, x.run.Ctx.Speech())
	if err != nil {
		return err
	}
	if !ok || !voiceplan.HasSpokenContent(text) {
		return skipped("speech_blocked")
	}
	allowed, err := x.p.speech.Allowed(x.ctx, x.run.BroadcasterID)
	if err != nil {
		return err
	}
	if !allowed {
		return limited("voice_unavailable")
	}
	req.Text = voiceplan.StripControls(text)
	return x.p.speech.Publish(x.ctx, req)
}
