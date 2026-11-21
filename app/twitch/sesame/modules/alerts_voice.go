// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"cmp"
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

type alertConfig map[string]string

func (cfg alertConfig) chatEnabled(kind voiceplan.AlertKind) bool {
	value := cfg[string(kind)+"Enabled"]
	if kind == "ads" {
		return explicitOn(value)
	}
	return alertOn(value)
}

type alertOutputs struct {
	c           *module.Context
	d           engine.Deps
	cfg         alertConfig
	audio       alertSpeech
	input       alertSpeechInput
	chat        bool
	wantsSpeech bool
}

func (a alertOutputs) send(ctx context.Context, emit module.Emit, render func() (alertLine, bool)) {
	speak := !a.chat && a.admitSpeech(ctx)
	if skipAlertOutputs(a.chat, speak) {
		return
	}
	line, ok := render()
	if !ok {
		return
	}
	if a.chat {
		a.emitChat(emit, line)
		speak = a.admitSpeech(ctx)
	}
	if speak {
		a.audio.publish(ctx, a.c, line)
	}
}

func (a *alertOutputs) admitSpeech(ctx context.Context) bool {
	return a.wantsSpeech && a.audio.admit(ctx, a.d, a.c, a.input)
}

func (a alertOutputs) emitChat(emit module.Emit, line alertLine) {
	kind := string(a.audio.kind)
	text := a.cfg[kind+"Message"]
	if text == "" {
		text = i18n.T(a.c.Locale, "alerts."+kind+".default")
	}
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: line.broadcasterID,
		Text:          a.c.Palette("alerts", tokenPairs(line.tokens)...).ExpandString(text),
	})
}

type alertSpeech struct {
	kind         voiceplan.AlertKind
	pub          engine.AlertVoicePublisher
	text         string
	style        string
	voice        string
	message      string
	liveOnly     bool
	readMessages bool
	minBits      int
}

func newAlertSpeech(d engine.Deps, c *module.Context, cfg alertConfig, kind voiceplan.AlertKind) (alertSpeech, bool) {
	s := alertSpeech{kind: kind, pub: d.AlertVoice}
	defaults, known := kind.Defaults()
	if !known || s.pub == nil {
		return s, false
	}
	if c.Env.Origin == "trial" || c.Env.EventID == "" {
		return s, false
	}
	key := string(kind) + "Tts"
	if !settingBool(cfg[key], defaults.Enabled) {
		return s, false
	}
	s.text = cmp.Or(cfg[key+"Message"], defaults.Message)
	s.style = cmp.Or(voiceplan.Style(cfg[key+"Style"]), voiceplan.Style(defaults.Style))
	if voice := cfg[key+"Voice"]; voiceplan.ValidVoice(voice) {
		s.voice = voice
	}
	s.liveOnly = settingBool(cfg["ttsLiveOnly"], voiceplan.DefaultLiveOnly)
	s.readMessages = settingBool(cfg["ttsReadMessages"], voiceplan.DefaultReadMessages)
	s.minBits = minimumBits(cfg)
	return s, true
}

func settingBool(value string, fallback bool) bool {
	switch value {
	case "true":
		return true
	case "false":
		return false
	default:
		return fallback
	}
}

func minimumBits(cfg alertConfig) int {
	bits, err := strconv.Atoi(cfg["ttsMinBits"])
	if err != nil || bits < 0 {
		return voiceplan.DefaultMinBits
	}
	return bits
}

type alertSpeechInput struct {
	bits    int
	message string
}

func speechInput(ev any) alertSpeechInput {
	switch ev := ev.(type) {
	case cheerEvent:
		return alertSpeechInput{bits: ev.Bits, message: ev.Message}
	case subscribeEvent:
		return alertSpeechInput{message: ev.Message.Text}
	default:
		return alertSpeechInput{}
	}
}

func (s *alertSpeech) admit(ctx context.Context, d engine.Deps, c *module.Context, in alertSpeechInput) bool {
	if s.kind == "cheer" && in.bits < s.minBits {
		return false
	}
	if !loyaltyLive(ctx, d, c.BroadcasterID, s.liveOnly) {
		return false
	}
	enabled, err := s.pub.Enabled(ctx, c.Env.BroadcasterUserID)
	if err != nil {
		moduleLog(d).Warn("voice allowance check failed", c.BID(), zap.Error(err))
		return false
	}
	if s.readMessages {
		s.message = speechPlainText(in.message)
	}
	return enabled
}

func (s alertSpeech) publish(ctx context.Context, c *module.Context, line alertLine) {
	text := tmpl.Expand(speechTemplate(s.text), func(tok tmpl.Token) (string, bool) {
		input, ok := line.tokens[tmpl.TokenKey(tok, "alerts")]
		return speechPlainText(input), ok
	})
	if s.message != "" {
		text += " " + s.message
	}
	job := voiceplan.AlertJob{
		EventID: c.Env.EventID, BroadcasterID: c.Env.BroadcasterUserID, Kind: s.kind,
		Text: text, Style: s.style, Voice: s.voice, CreatedAt: time.Now().UTC(),
	}
	if err := s.pub.Publish(ctx, job); err != nil {
		c.Log.Warn("voice job publication failed", c.BID(), zap.Error(err))
	}
}

var templateTag = regexp.MustCompile(`<[^<>]*>|[<>]`)

func speechTemplate(text string) string {
	return templateTag.ReplaceAllStringFunc(text, func(tag string) string {
		if len(tag) > 2 && !strings.ContainsAny(tag, "{}") {
			return tag
		}
		return strings.Trim(tag, "<>")
	})
}

func speechPlainText(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case r == '<', r == '>', unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		default:
			return r
		}
	}, value)
	return strings.Join(strings.Fields(value), " ")
}
