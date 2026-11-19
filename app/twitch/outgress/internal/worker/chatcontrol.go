// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"slices"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const whisperMaxRunes = 500

type chatMode struct {
	flag        string
	durationKey string
	fallback    int
	min, max    int
	allowed     []int
}

var chatModes = map[string]chatMode{
	outgress.ChatModeSlow:       {flag: "slow_mode", durationKey: "slow_mode_wait_time", fallback: 30, min: 3, max: 120},
	outgress.ChatModeFollower:   {flag: "follower_mode", durationKey: "follower_mode_duration", min: 0, max: 129600},
	outgress.ChatModeEmote:      {flag: "emote_mode"},
	outgress.ChatModeSubscriber: {flag: "subscriber_mode"},
	outgress.ChatModeUnique:     {flag: "unique_chat_mode"},
	outgress.ChatModeDelay:      {flag: "non_moderator_chat_delay", durationKey: "non_moderator_chat_delay_duration", fallback: 2, allowed: []int{2, 4, 6}},
}

func (m chatMode) settings(req outgress.ChatSettings) map[string]any {
	out := map[string]any{m.flag: req.Enabled}
	if req.Enabled && m.durationKey != "" {
		out[m.durationKey] = m.duration(req.Duration)
	}
	return out
}

func (m chatMode) duration(requested int) int {
	switch {
	case slices.Contains(m.allowed, requested):
		return requested
	case len(m.allowed) > 0, requested == 0 && m.fallback != 0:
		return m.fallback
	default:
		return min(max(requested, m.min), m.max)
	}
}

func (w *Worker) processChatSettings(ctx context.Context, payload *outgress.Message) error {
	var req outgress.ChatSettings
	if !w.decodeTyped(payload, &req) {
		return nil
	}
	mode, known := chatModes[req.Mode]
	if !known {
		w.log.Error("dropping chat settings: unknown mode",
			zap.String("broadcaster_id", payload.BroadcasterID), zap.String("mode", req.Mode))
		return nil
	}
	return w.asModerator(ctx, payload, func(ctx context.Context, botID string) error {
		return w.twitch.UpdateChatSettings(ctx, twitch.ChannelModerator{BroadcasterID: payload.BroadcasterID, ModeratorID: botID}, mode.settings(req))
	})
}

func (w *Worker) processClearChat(ctx context.Context, payload *outgress.Message) error {
	return w.asModerator(ctx, payload, func(ctx context.Context, botID string) error {
		return w.twitch.ClearChat(ctx, twitch.ChannelModerator{BroadcasterID: payload.BroadcasterID, ModeratorID: botID})
	})
}

func (w *Worker) processAdSnooze(ctx context.Context, payload *outgress.Message) error {
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBroadcaster, call: func(ctx context.Context) error {
		return w.twitch.SnoozeNextAd(ctx, payload.BroadcasterID)
	}})
}

func (w *Worker) processWhisper(ctx context.Context, payload *outgress.Message) error {
	var req outgress.Whisper
	if !w.decodeTyped(payload, &req) || req.Message == "" {
		return nil
	}
	targetID, err := w.resolveLogin(ctx, payload, payload.To)
	if err != nil || targetID == "" {
		return err
	}
	message := truncateRunes(req.Message, whisperMaxRunes)
	return w.asModerator(ctx, payload, func(ctx context.Context, botID string) error {
		return w.twitch.SendWhisper(ctx, payload.BroadcasterID, twitch.WhisperRequest{FromID: botID, ToID: targetID, Message: message})
	})
}

func (w *Worker) asModerator(ctx context.Context, payload *outgress.Message, call func(ctx context.Context, botID string) error) error {
	botID, ok := w.botIdentity(payload.Type, payload)
	if !ok {
		return nil
	}
	return w.runTyped(ctx, payload, typedJob{as: outgress.AsBot, call: func(ctx context.Context) error {
		return call(ctx, botID)
	}})
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
