// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/voiceplan"

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
	a.sendChat(emit, render)
	if a.admitSpeech(ctx) {
		a.audio.publish(ctx, a.c)
	}
}

func (a alertOutputs) sendChat(emit module.Emit, render func() (alertLine, bool)) {
	if !a.chat {
		return
	}
	if line, ok := render(); ok {
		a.emitChat(emit, line)
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
	kind     voiceplan.AlertKind
	pub      engine.AlertVoicePublisher
	style    string
	voice    string
	speaker  string
	message  string
	liveOnly bool
	minBits  int
}

func spokenEvent(eventType string) bool {
	return eventType == "channel.cheer" || eventType == "channel.subscription.message"
}

func newAlertSpeech(d engine.Deps, c *module.Context, cfg alertConfig, kind voiceplan.AlertKind) (alertSpeech, bool) {
	s := alertSpeech{kind: kind, pub: d.AlertVoice}
	defaults, known := kind.Defaults()
	if !known || s.pub == nil {
		return s, false
	}
	if !spokenEvent(c.Env.Type) || c.Env.Origin == "trial" {
		return s, false
	}
	if c.Env.EventID == "" {
		return s, false
	}
	key := string(kind) + "Tts"
	if !settingBool(cfg[key], defaults.Enabled) {
		return s, false
	}
	s.style = cmp.Or(voiceplan.Style(cfg[key+"Style"]), voiceplan.Style(defaults.Style))
	if voice := cfg[key+"Voice"]; voiceplan.ValidVoice(voice) {
		s.voice = voice
	}
	s.liveOnly = settingBool(cfg["ttsLiveOnly"], voiceplan.DefaultLiveOnly)
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
	speaker string
	message string
	bits    int
}

func speechInput(c *module.Context, ev any) alertSpeechInput {
	switch ev := ev.(type) {
	case cheerEvent:
		return cheerSpeechInput(c, ev)
	case subscribeEvent:
		return subSpeechInput(c, ev)
	default:
		return alertSpeechInput{}
	}
}

func cheerSpeechInput(c *module.Context, ev cheerEvent) alertSpeechInput {
	if ev.BroadcasterUserID == "" {
		return alertSpeechInput{}
	}
	speaker := i18n.T(c.Locale, "alerts.voice.anonymous")
	if !ev.IsAnonymous {
		speaker = chatName(ev.UserName, ev.UserLogin)
	}
	return alertSpeechInput{speaker: speaker, message: ev.Message, bits: ev.Bits}
}

func subSpeechInput(c *module.Context, ev subscribeEvent) alertSpeechInput {
	if c.Env.Type != "channel.subscription.message" || ev.skip() {
		return alertSpeechInput{}
	}
	if ev.BroadcasterUserID == "" {
		return alertSpeechInput{}
	}
	return alertSpeechInput{speaker: ev.user(), message: ev.Message.Text}
}

func (s *alertSpeech) admit(ctx context.Context, d engine.Deps, c *module.Context, in alertSpeechInput) bool {
	if s.kind == "cheer" && in.bits < s.minBits {
		return false
	}
	s.message = s.viewerMessage(c, in.message)
	if s.message == "" {
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
	s.speaker = in.speaker
	return enabled
}

var cheermoteToken = regexp.MustCompile(`^[A-Za-z]+[0-9]+$`)

func (s *alertSpeech) viewerMessage(c *module.Context, message string) string {
	text := speechPlainText(message)
	if s.kind == "cheer" {
		words := slices.DeleteFunc(strings.Fields(text), isCheermoteWord)
		text = strings.Join(words, " ")
	}
	if !hasSpokenContent(text) || blockedSpeech(c, text) {
		return ""
	}
	return text
}

func isCheermoteWord(word string) bool {
	return cheermoteToken.MatchString(strings.TrimFunc(word, unicode.IsPunct))
}

func hasSpokenContent(text string) bool {
	return strings.ContainsFunc(text, isSpokenRune)
}

func isSpokenRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func blockedSpeech(c *module.Context, text string) bool {
	if text == "" || c.BlocksText == nil {
		return false
	}
	return c.BlocksText(text)
}

func (s alertSpeech) publish(ctx context.Context, c *module.Context) {
	speaker := cmp.Or(speechPlainText(s.speaker), i18n.T(c.Locale, "alerts.voice.anonymous"))
	text := fmt.Sprintf(i18n.T(c.Locale, "alerts.voice.said"), speaker, s.message)
	job := voiceplan.AlertJob{
		EventID: c.Env.EventID, BroadcasterID: c.Env.BroadcasterUserID, Kind: s.kind,
		Text: stripSpeechControls(text), Style: s.style, Voice: s.voice, CreatedAt: time.Now().UTC(),
	}
	if err := s.pub.Publish(ctx, job); err != nil {
		c.Log.Warn("voice job publication failed", c.BID(), zap.Error(err))
	}
}

func stripSpeechControls(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r) && unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		default:
			return r
		}
	}, text)
}

var speechAngleBrackets = strings.NewReplacer("<", " ", ">", " ")

func speechPlainText(value string) string {
	value = speechAngleBrackets.Replace(stripSpeechControls(value))
	return strings.Join(strings.Fields(value), " ")
}
