// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

func TTS(d engine.Deps) module.Module {
	m := module.NewModule("", module.KindCore)
	cmd := ttsCommand{proj: d.Proj, speech: d.Speech(), log: moduleLog(d)}
	m.Command(voiceplan.ChatCommand).Everyone().LiveOnly().Yields().When(cmd.claims).
		UserCooldown(voiceplan.ChatDefaultUserCooldown * time.Second).Run(cmd.run)
	return m.Build()
}

type ttsCommand struct {
	proj   projection.Reader
	speech engine.SpeechQueue
	log    *zap.Logger
}

type ttsSettings struct {
	MaxChars    string `json:"maxChars"`
	Voice       string `json:"voice"`
	Style       string `json:"style"`
	PrefixName  string `json:"prefixName"`
	ReplyDenied string `json:"replyDenied"`
}

type ttsDenial string

const (
	ttsBlocked   ttsDenial = "tts.blocked"
	ttsNoMinutes ttsDenial = "tts.noMinutes"
)

func (cmd ttsCommand) claims(ctx context.Context, c *module.Context) bool {
	lookup := engine.ModuleLookup{Proj: cmd.proj, BroadcasterID: c.BroadcasterID, Name: voiceplan.ChatCommand, Absent: engine.ModuleOff}
	_, state, err := lookup.Resolve(ctx)
	if err != nil || state != engine.ModuleOn {
		return false
	}
	return cmd.speech.Premium(ctx, c.BroadcasterID)
}

func (cmd ttsCommand) run(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
	var cfg ttsSettings
	_ = c.Decode(&cfg)
	if !speechAvailable(c) {
		return nil
	}
	in := viewerSpeech{text: speechText(args), name: speechText(c.Env.ChatterName()), viewerID: c.Env.ChatterUserID}
	opts := cfg.options()
	opts.queue = cmd.speech
	req, ok, err := opts.request(ctx, c, voiceplan.SpeechChat, in)
	if err != nil {
		return err
	}
	if !ok {
		cfg.deny(c, emit, ttsBlocked)
		return nil
	}
	allowed, err := cmd.speech.Allowed(ctx, c.BroadcasterID)
	if err != nil || !allowed {
		cmd.logRefusal(c, err)
		cfg.deny(c, emit, ttsNoMinutes)
		return nil
	}
	req.SourceMessageID = c.Env.MsgID
	return cmd.speech.Publish(ctx, req)
}

func (cfg ttsSettings) options() speechOptions {
	return speechOptions{
		voice: cfg.Voice, style: cfg.Style, maxChars: ttsMaxChars(cfg.MaxChars),
		prefixName: settingBool(cfg.PrefixName, voiceplan.DefaultPrefixName),
	}
}

func ttsMaxChars(value string) int {
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 {
		return voiceplan.ChatDefaultMaxChars
	}
	return min(limit, voiceplan.MaxSpeechRunes)
}

func (cmd ttsCommand) logRefusal(c *module.Context, err error) {
	if err != nil {
		cmd.log.Warn("tts allowance check failed", c.BID(), zap.Error(err))
	}
}

func (cfg ttsSettings) deny(c *module.Context, emit module.Emit, key ttsDenial) {
	if !settingBool(cfg.ReplyDenied, false) {
		return
	}
	emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: i18n.T(c.Locale, string(key))})
}
