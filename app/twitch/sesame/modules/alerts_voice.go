// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

type speechSkipReason string

const (
	speechUnavailable speechSkipReason = "no_link_or_not_allowed"
	speechOffline     speechSkipReason = "offline"
	speechMinBits     speechSkipReason = "min_bits"
	speechEmpty       speechSkipReason = "empty_message"
	speechBlocked     speechSkipReason = "blocked"
	speechTrial       speechSkipReason = "trial"
	speechToggleOff   speechSkipReason = "toggle_off"
	speechNoIdentity  speechSkipReason = "missing_identity"
)

type alertSpeech struct {
	d        *engine.Deps
	c        *module.Context
	kind     voiceplan.AlertKind
	identity string
	traceID  string
	cfg      alertConfig
}

func AlertVoice(d engine.Deps) module.Module {
	return module.NewModule("", module.KindCore).
		ProtectReplay().
		On("channel.cheer", onAlertSpeech(d, "cheer", cheerSpeechInput)).
		On("channel.subscription.message", onAlertSpeech(d, "sub", subSpeechInput)).
		Build()
}

func onAlertSpeech[T any](d engine.Deps, kind voiceplan.AlertKind, input func(*module.Context, T) alertSpeechInput) module.EventHandler {
	return func(ctx context.Context, c *module.Context, _ module.Emit) error {
		monitor.Keep(ctx)
		s := alertSpeech{
			d: &d, c: c, kind: kind, traceID: bus.OriginTraceID(ctx),
			identity: cmp.Or(c.Env.EventID, c.Env.MsgID),
		}
		if reason := s.preflight(); reason != "" {
			return s.skip(ctx, reason)
		}
		if err := s.loadConfig(ctx); err != nil {
			c.Log.Warn("voice alert settings could not be loaded", c.BID(), zap.Error(err))
			return s.skip(ctx, speechUnavailable)
		}
		defaults, _ := kind.Defaults()
		if !settingBool(s.cfg[s.configKey()], defaults.Enabled) {
			return s.skip(ctx, speechToggleOff)
		}
		var ev T
		if len(c.Env.Event) == 0 {
			return s.skip(ctx, speechEmpty)
		}
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		return s.handle(ctx, input(c, ev))
	}
}

func (s alertSpeech) preflight() speechSkipReason {
	switch {
	case s.c.Env.Origin == "trial":
		return speechTrial
	case s.d.AlertVoice == nil:
		return speechUnavailable
	case s.identity == "" && s.traceID == "":
		return speechNoIdentity
	default:
		return ""
	}
}

func (s *alertSpeech) loadConfig(ctx context.Context) error {
	lookup := engine.ModuleLookup{Proj: s.d.Proj, BroadcasterID: s.c.BroadcasterID, Name: "alerts"}
	view, _, err := lookup.Resolve(ctx)
	if err != nil || len(view.Configs) == 0 {
		return err
	}
	var raw map[string]any
	if err := codec.Unmarshal(view.Configs, &raw); err != nil {
		return err
	}
	s.cfg = alertConfig{}
	for key, value := range raw {
		if text, ok := value.(string); ok {
			s.cfg[key] = text
		}
	}
	return nil
}

func (s alertSpeech) configKey() string { return string(s.kind) + "Tts" }

func (s alertSpeech) broadcasterID() string {
	return strconv.FormatUint(s.c.BroadcasterID, 10)
}

func (s alertSpeech) skip(ctx context.Context, reason speechSkipReason) error {
	monitor.TxnLogger(ctx, s.c.Log).Info("voice alert skipped", s.c.BID(), zap.String("kind", string(s.kind)),
		zap.String("reason", string(reason)), zap.String("trace_id", s.traceID))
	return nil
}

func (s alertSpeech) handle(ctx context.Context, in alertSpeechInput) error {
	in.message = s.viewerMessage(in.message)
	if reason := s.skipReason(ctx, in); reason != "" {
		return s.skip(ctx, reason)
	}
	job := s.job(in)
	if err := s.d.AlertVoice.Publish(ctx, job); err != nil {
		return fmt.Errorf("publish voice job %s: %w", job.EventID, err)
	}
	monitor.TxnLogger(ctx, s.c.Log).Info("voice job published", s.c.BID(), zap.String("kind", string(job.Kind)),
		zap.String("event_id", job.EventID), zap.String("trace_id", job.TraceID))
	return nil
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

func subSpeechInput(_ *module.Context, ev subscribeEvent) alertSpeechInput {
	if ev.skip() || ev.BroadcasterUserID == "" {
		return alertSpeechInput{}
	}
	return alertSpeechInput{speaker: ev.user(), message: ev.Message.Text}
}

func (s alertSpeech) skipReason(ctx context.Context, in alertSpeechInput) speechSkipReason {
	switch {
	case s.kind == "cheer" && in.bits < minimumBits(s.cfg):
		return speechMinBits
	case !hasSpokenContent(in.message):
		return speechEmpty
	case blockedSpeech(s.c, in.message):
		return speechBlocked
	case !loyaltyLive(ctx, *s.d, s.c.BroadcasterID, settingBool(s.cfg["ttsLiveOnly"], voiceplan.DefaultLiveOnly)):
		return speechOffline
	default:
		return s.allowanceReason(ctx)
	}
}

func (s alertSpeech) allowanceReason(ctx context.Context) speechSkipReason {
	enabled, err := s.d.AlertVoice.Enabled(ctx, s.broadcasterID())
	if err != nil {
		s.c.Log.Warn("voice allowance check failed", s.c.BID(), zap.Error(err))
	}
	if err != nil || !enabled {
		return speechUnavailable
	}
	return ""
}

var cheermoteToken = regexp.MustCompile(`^[A-Za-z]+[0-9]+$`)

func (s alertSpeech) viewerMessage(message string) string {
	text := speechPlainText(message)
	if s.kind == "cheer" {
		return strings.Join(slices.DeleteFunc(strings.Fields(text), isCheermoteWord), " ")
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
	return c.BlocksText != nil && c.BlocksText(text)
}

func (s alertSpeech) job(in alertSpeechInput) voiceplan.AlertJob {
	key := s.configKey()
	defaults, _ := s.kind.Defaults()
	style := cmp.Or(voiceplan.Style(s.cfg[key+"Style"]), voiceplan.Style(defaults.Style))
	displayName := cmp.Or(speechPlainText(in.speaker), i18n.T(s.c.Locale, "alerts.voice.anonymous"))
	text := fmt.Sprintf(i18n.T(s.c.Locale, "alerts.voice.said"), displayName, in.message)
	return voiceplan.AlertJob{
		EventID: s.jobID(), TraceID: s.traceID, BroadcasterID: s.broadcasterID(), Kind: s.kind,
		Text: stripSpeechControls(text), Style: style, Voice: speechVoice(s.cfg[key+"Voice"]), CreatedAt: time.Now().UTC(),
	}
}

func (s alertSpeech) jobID() string {
	if s.traceID != "" {
		return s.traceID
	}
	digest := sha256.Sum256([]byte(s.identity))
	return hex.EncodeToString(digest[:16])
}

func speechVoice(voice string) string {
	if voiceplan.ValidVoice(voice) {
		return voice
	}
	return ""
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
