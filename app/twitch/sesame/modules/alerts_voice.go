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
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

type speechSkipReason string

const (
	speechUnavailable    speechSkipReason = "no_link_or_not_allowed"
	speechBusUnavailable speechSkipReason = "unavailable"
	speechOffline        speechSkipReason = "offline"
	speechMinBits        speechSkipReason = "min_bits"
	speechEmpty          speechSkipReason = "empty_message"
	speechBlocked        speechSkipReason = "blocked"
	speechTrial          speechSkipReason = "trial"
	speechToggleOff      speechSkipReason = "toggle_off"
	speechNoIdentity     speechSkipReason = "missing_identity"
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
	case !s.d.Speech().Available():
		return speechBusUnavailable
	case s.identity == "" && s.traceID == "":
		return speechNoIdentity
	default:
		return ""
	}
}

func (s *alertSpeech) loadConfig(ctx context.Context) error {
	cfg, err := readAlertConfig(ctx, s.d, s.c.BroadcasterID)
	s.cfg = cfg
	return err
}

func readAlertConfig(ctx context.Context, d *engine.Deps, broadcasterID uint64) (alertConfig, error) {
	lookup := engine.ModuleLookup{Proj: d.Proj, BroadcasterID: broadcasterID, Name: "alerts"}
	view, _, err := lookup.Resolve(ctx)
	if err != nil || len(view.Configs) == 0 {
		return nil, err
	}
	var raw map[string]any
	if err := codec.Unmarshal(view.Configs, &raw); err != nil {
		return nil, err
	}
	cfg := alertConfig{}
	for key, value := range raw {
		if text, ok := value.(string); ok {
			cfg[key] = text
		}
	}
	return cfg, nil
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
	reason, err := s.skipReason(ctx, &in)
	if err != nil {
		return err
	}
	if reason != "" {
		return s.skip(ctx, reason)
	}
	return s.publish(ctx, s.job(in, s.jobID()))
}

func (s alertSpeech) publish(ctx context.Context, job voiceplan.AlertJob) error {
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
	speaker  string
	message  string
	viewerID string
	bits     int
}

func cheerSpeechInput(c *module.Context, ev cheerEvent) alertSpeechInput {
	if ev.BroadcasterUserID == "" {
		return alertSpeechInput{}
	}
	speaker := i18n.T(c.Locale, "alerts.voice.anonymous")
	viewerID := ""
	if !ev.IsAnonymous {
		speaker, viewerID = chatName(ev.UserName, ev.UserLogin), ev.UserID
	}
	return alertSpeechInput{speaker: speaker, message: ev.Message, viewerID: viewerID, bits: ev.Bits}
}

func subSpeechInput(_ *module.Context, ev subscribeEvent) alertSpeechInput {
	if ev.skip() || ev.BroadcasterUserID == "" {
		return alertSpeechInput{}
	}
	return alertSpeechInput{speaker: ev.user(), message: ev.Message.Text, viewerID: ev.UserID}
}

func (s alertSpeech) contentReason(in alertSpeechInput) speechSkipReason {
	switch {
	case s.kind == "cheer" && in.bits < minimumBits(s.cfg):
		return speechMinBits
	case !voiceplan.HasSpokenContent(in.message):
		return speechEmpty
	default:
		return ""
	}
}

func (s alertSpeech) skipReason(ctx context.Context, in *alertSpeechInput) (speechSkipReason, error) {
	if reason := s.contentReason(*in); reason != "" {
		return reason, nil
	}
	message, allowed, err := s.d.Speech().Screen(ctx, s.subject(*in), moderation.SpeechInput{Name: voiceplan.PlainText(in.speaker), Message: in.message}, s.c.Speech())
	in.message = message
	switch {
	case err != nil:
		return "", err
	case !allowed:
		return speechBlocked, nil
	case !voiceplan.HasSpokenContent(in.message):
		return speechEmpty, nil
	case !loyaltyLive(ctx, *s.d, s.c.BroadcasterID, settingBool(s.cfg["ttsLiveOnly"], voiceplan.DefaultLiveOnly)):
		return speechOffline, nil
	default:
		return s.allowanceReason(ctx), nil
	}
}

func (s alertSpeech) subject(in alertSpeechInput) engine.SpeechSubject {
	return engine.SpeechSubject{BroadcasterID: s.c.BroadcasterID, ViewerID: in.viewerID, Source: voiceplan.SourceViewer}
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
	text := voiceplan.PlainText(message)
	if s.kind == "cheer" {
		return strings.Join(slices.DeleteFunc(strings.Fields(text), isCheermoteWord), " ")
	}
	return text
}

func isCheermoteWord(word string) bool {
	return cheermoteToken.MatchString(strings.TrimFunc(word, unicode.IsPunct))
}

func (s alertSpeech) job(in alertSpeechInput, jobID string) voiceplan.AlertJob {
	key := s.configKey()
	defaults, _ := s.kind.Defaults()
	style := cmp.Or(voiceplan.Style(s.cfg[key+"Style"]), voiceplan.Style(defaults.Style))
	displayName := cmp.Or(voiceplan.PlainText(in.speaker), i18n.T(s.c.Locale, "alerts.voice.anonymous"))
	text := fmt.Sprintf(i18n.T(s.c.Locale, "alerts.voice.said"), displayName, in.message)
	return voiceplan.AlertJob{
		EventID: jobID, TraceID: s.traceID, BroadcasterID: s.broadcasterID(), Kind: s.kind,
		Text: voiceplan.StripControls(text), Style: style, Voice: speechVoice(s.cfg[key+"Voice"]), CreatedAt: time.Now().UTC(),
		ViewerID: in.viewerID, Source: voiceplan.SourceViewer,
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
