// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"cmp"
	"context"
	"fmt"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
)

type speechText string

type viewerSpeech struct {
	text speechText
	name speechText
}

type speechOptions struct {
	voice      string
	style      string
	prefixName bool
	maxChars   int
}

func (opts speechOptions) request(c *module.Context, kind voiceplan.AlertKind, in viewerSpeech) (engine.SpeechRequest, bool) {
	text := voiceplan.TrimRunes(voiceplan.PlainText(string(in.text)), opts.maxChars)
	if !voiceplan.HasSpokenContent(text) || blockedSpeech(c, text) {
		return engine.SpeechRequest{}, false
	}
	if name := voiceplan.PlainText(string(in.name)); opts.prefixName && name != "" {
		text = fmt.Sprintf(i18n.T(c.Locale, "alerts.voice.said"), name, text)
	}
	return engine.SpeechRequest{
		Kind: kind, BroadcasterID: c.BroadcasterID, Identity: cmp.Or(c.Env.EventID, c.Env.MsgID),
		Text: voiceplan.StripControls(text), Style: voiceplan.Style(opts.style), Voice: speechVoice(opts.voice),
	}, true
}

func speechAvailable(c *module.Context) bool {
	return c.Env.Origin != "trial" && cmp.Or(c.Env.EventID, c.Env.MsgID) != ""
}

func withTrace(ctx context.Context, req engine.SpeechRequest) engine.SpeechRequest {
	req.TraceID = bus.OriginTraceID(ctx)
	return req
}
