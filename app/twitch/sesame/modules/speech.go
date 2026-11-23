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
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
)

type speechText string

type viewerSpeech struct {
	text     speechText
	name     speechText
	viewerID string
}

type speechOptions struct {
	queue      engine.SpeechQueue
	voice      string
	style      string
	prefixName bool
	maxChars   int
}

func (opts speechOptions) request(ctx context.Context, c *module.Context, kind voiceplan.AlertKind, in viewerSpeech) (engine.SpeechRequest, bool, error) {
	text := voiceplan.TrimRunes(voiceplan.PlainText(string(in.text)), opts.maxChars)
	if !voiceplan.HasSpokenContent(text) {
		return engine.SpeechRequest{}, false, nil
	}
	name := voiceplan.PlainText(string(in.name))
	req := engine.SpeechRequest{
		Kind: kind, BroadcasterID: c.BroadcasterID, Identity: cmp.Or(c.Env.EventID, c.Env.MsgID), TraceID: bus.OriginTraceID(ctx),
		Style: voiceplan.Style(opts.style), Voice: speechVoice(opts.voice), ViewerID: in.viewerID, Source: voiceplan.SourceViewer,
	}
	text, ok, err := opts.queue.Screen(ctx, req.Subject(), moderation.SpeechInput{Name: name, Message: text}, c.Speech())
	if err != nil || !ok {
		return engine.SpeechRequest{}, false, err
	}
	if !voiceplan.HasSpokenContent(text) {
		return engine.SpeechRequest{}, false, nil
	}
	if opts.prefixName && name != "" {
		text = fmt.Sprintf(i18n.T(c.Locale, "alerts.voice.said"), name, text)
	}
	req.Text = voiceplan.StripControls(text)
	return req, true, nil
}

func speechAvailable(c *module.Context) bool {
	return c.Env.Origin != "trial" && cmp.Or(c.Env.EventID, c.Env.MsgID) != ""
}
