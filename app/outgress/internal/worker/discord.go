// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"time"

	"ItsBagelBot/app/outgress/internal/discord"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// Discord actions pay two gates before the REST call, mirroring the YouTube
// shape: a per-channel pacing bucket (Discord's per-channel buckets allow
// roughly 5 sends / 5s; pacing at 1/s keeps announcements readable and never
// trips them) and a fleet-shared global bucket (the bot's global limit is
// ~50 req/s across ALL channels — thousands of paced channels could in theory
// sum past it, so a shared ceiling guards it). Results classify into drop
// (nil) versus nack (error) exactly like the Helix paths.
const (
	// discordChatPacingCapacity/Window: 5 tokens per 5s per channel, refilled
	// at 1/s. The window is env-tunable via DISCORD_CHAT_MIN_INTERVAL_MS (see
	// SetDiscordPacing) for operators who want stricter pacing.
	discordChatPacingCapacity = 5.0
	discordChatPacingWindow   = 5.0

	// discordGlobalCapacity/Window: 45/min of the bot's real ~50 req/s global
	// budget, leaving headroom so bursts never touch Discord's global 429.
	discordGlobalCapacity = 45.0
	discordGlobalWindow   = 60.0

	// discordContentMaxBytes bounds content past this many bytes as malformed
	// or abusive. Discord's own limit is 2000 characters; a UTF-8 bound of
	// 2048 bytes admits any legal message while rejecting floods cheaply,
	// before the REST call is spent on a guaranteed 400.
	discordContentMaxBytes = 2_048
)

// discordChatPacingSpec is rebuilt once at wiring from
// DISCORD_CHAT_MIN_INTERVAL_MS (setDiscordPacing); the var keeps the default
// so tests need no wiring.
var discordChatPacingSpec = ratelimit.NewSpec(discordChatPacingCapacity,
	discordChatPacingCapacity/discordChatPacingWindow)

// discordGlobalSpec is fixed: it backs the bot's real global limit, which no
// env knob should be able to push past.
var discordGlobalSpec = ratelimit.NewSpec(discordGlobalCapacity, discordGlobalCapacity/discordGlobalWindow)

// discordAPI is the slice of the Discord REST API the handlers fire;
// *discord.Client satisfies it in production and a recorder stands in for tests.
type discordAPI interface {
	SendMessage(ctx context.Context, channelID, content string, tts bool) error
}

// processDiscordChat posts one message into the target channel
// (Message.ChannelID). The enabled/disabled decision belongs upstream
// (sesame), mirroring both chat paths; outgress only gates and fires. There
// is deliberately no channel resolution fallback: producers carry the
// snowflake from their own configuration, because Discord has no
// per-broadcaster identity outgress could look one up from.
func (w *Worker) processDiscordChat(ctx context.Context, payload *outgress.Message) error {
	var body struct {
		Content string `json:"content"`
		TTS     bool   `json:"tts"`
	}
	if err := codec.Unmarshal(payload.Payload, &body); err != nil {
		w.log.Error("dropping discord chat: malformed payload",
			zap.String("channel_id", payload.ChannelID), zap.Error(err))
		return nil
	}
	if body.Content == "" || len(body.Content) > discordContentMaxBytes {
		w.log.Error("dropping discord chat: empty or oversized content",
			zap.String("channel_id", payload.ChannelID),
			zap.Int("bytes", len(body.Content)))
		return nil
	}
	if payload.ChannelID == "" {
		w.log.Error("dropping discord chat: no target channel id")
		return nil
	}

	if err := w.take(ctx, discordChatPacingSpec.ForDynamicKey(
		"ratelimit:discord:chat:", "discord:chat", payload.ChannelID)); err != nil {
		return err
	}
	// Global last: a shared-bucket refusal here means the fleet as a whole is
	// saturated, which redelivery paces through without starving any single
	// channel's order.
	if err := w.take(ctx, discordGlobalSpec.ForKey("ratelimit:discord:global")); err != nil {
		return err
	}

	return w.classifyDiscordResult(ctx, payload,
		w.discord.SendMessage(ctx, payload.ChannelID, body.Content, body.TTS))
}

// classifyDiscordResult maps client errors onto the lane discipline:
//
//	ErrAuth / ErrForbidden / ErrChannelNotFound / ErrBadRequest -> drop
//	ErrRateLimited, network errors, anything else               -> nack
func (w *Worker) classifyDiscordResult(ctx context.Context, payload *outgress.Message, err error) error {
	switch {
	case err == nil:
		return nil

	case isDiscordPermanent(err):
		w.log.Error("dropping discord action (permanent)",
			zap.String("channel_id", payload.ChannelID),
			zap.String("type", payload.Type),
			zap.Error(err))
		noticeError(ctx, err)
		return nil

	default:
		w.log.Warn("discord action failed, will retry",
			zap.String("channel_id", payload.ChannelID),
			zap.String("type", payload.Type),
			zap.Error(err))
		return err
	}
}

func isDiscordPermanent(err error) bool {
	switch err {
	case discord.ErrAuth, discord.ErrForbidden,
		discord.ErrChannelNotFound, discord.ErrBadRequest:
		return true
	}
	return false
}

// SetDiscordPacing retunes the per-channel pacing bucket from configuration.
// Wiring calls it once before any consumer starts; specs are pre-encoded, so
// this rebuilds the package-level var rather than mutating it.
func SetDiscordPacing(interval time.Duration) {
	discordChatPacingSpec = ratelimit.NewSpec(discordChatPacingCapacity,
		discordChatPacingCapacity/interval.Seconds())
}
