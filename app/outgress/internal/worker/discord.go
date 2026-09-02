// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"unicode/utf8"

	"ItsBagelBot/app/outgress/internal/discord"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// Discord actions pay two gates before the REST call, mirroring the Helix
// chat shape: a per-channel pacing bucket (Discord's per-channel buckets
// allow roughly 5 sends / 5s; pacing at 1/s keeps announcements readable and
// never trips them) and a fleet-shared global bucket.
const (
	// discordChatPacingCapacity/Window: 5 tokens per 5s per channel, refilled
	// at 1/s.
	discordChatPacingCapacity = 5.0
	discordChatPacingWindow   = 5.0

	// discordGlobalCapacity/Window: 45 requests per SECOND against the bot's
	// real 50 req/s global budget, leaving headroom so bursts never touch
	// Discord's global 429. The first cut wrote 45 per 60s (0.75 req/s): at
	// that rate every fleet-wide send past the 45th in a minute nacked, and
	// with MaxDeliver 3 on a 5s lane those announcements were dropped.
	discordGlobalCapacity = 45.0
	discordGlobalWindow   = 1.0

	// discordContentMaxRunes is Discord's own 2000-character limit, measured
	// in runes: a byte bound admitted 2048-byte ASCII bodies that ate a
	// guaranteed 400 on the REST call.
	discordContentMaxRunes = 2000
)

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

// HasDiscord reports whether a REST client is attached (bot token set).
func (w *Worker) HasDiscord() bool { return w.discord != nil }

// processDiscordChat posts one message into the target channel
// (Message.ChannelID). The enabled/disabled decision belongs upstream
// (sesame), mirroring both chat paths; outgress only gates and fires. There
// is deliberately no channel resolution fallback: producers carry the
// snowflake from their own configuration, because Discord has no
// per-broadcaster identity outgress could look one up from.
func (w *Worker) processDiscordChat(ctx context.Context, payload *outgress.Message) error {
	if w.discord == nil {
		w.log.Error("dropping discord chat: no client")
		return nil
	}
	body, ok := w.decodeDiscordChat(payload)
	if !ok {
		return nil
	}
	if err := w.take(ctx, discordChatPacingSpec.ForDynamicKey(
		"ratelimit:discord:chat:", "discord:chat", payload.ChannelID)); err != nil {
		return err
	}
	// Global last: a shared-bucket refusal here means the fleet as a whole is
	// saturated, which redelivery paces through without starving any single
	// channel's order.
	if err := w.takeDiscordGlobal(ctx); err != nil {
		return err
	}
	return w.classifyDiscordResult(ctx, payload,
		w.discord.SendMessage(ctx, payload.ChannelID, body.Content, body.TTS))
}

type discordChatBody struct {
	Content string `json:"content"`
	TTS     bool   `json:"tts"`
}

// decodeDiscordChat validates the payload; a false return was logged and
// must be acked (dropped), never retried.
func (w *Worker) decodeDiscordChat(payload *outgress.Message) (discordChatBody, bool) {
	var body discordChatBody
	if err := codec.Unmarshal(payload.Payload, &body); err != nil {
		w.log.Error("dropping discord chat: malformed payload",
			zap.String("channel_id", payload.ChannelID), zap.Error(err))
		return body, false
	}
	if !discordContentOK(body.Content) {
		w.log.Error("dropping discord chat: empty or oversized content",
			zap.String("channel_id", payload.ChannelID),
			zap.Int("runes", utf8.RuneCountInString(body.Content)))
		return body, false
	}
	if payload.ChannelID == "" {
		w.log.Error("dropping discord chat: no target channel id")
		return body, false
	}
	return body, true
}

func discordContentOK(content string) bool {
	return content != "" && utf8.RuneCountInString(content) <= discordContentMaxRunes
}

// takeDiscordGlobal pays one token from the fleet-wide bucket. Every REST
// call outgress makes on the bot token goes through here: chat copies, live
// and clip embeds, operator posts. A mass go-live burst or an ingress replay
// fans out through the same budget the chat lanes pay.
func (w *Worker) takeDiscordGlobal(ctx context.Context) error {
	return w.take(ctx, discordGlobalSpec.ForKey("ratelimit:discord:global"))
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
			zap.Duration("retry_after", discord.RetryAfterOf(err)),
			zap.Error(err))
		return err
	}
}

func isDiscordPermanent(err error) bool {
	return errors.Is(err, discord.ErrAuth) ||
		errors.Is(err, discord.ErrForbidden) ||
		errors.Is(err, discord.ErrChannelNotFound) ||
		errors.Is(err, discord.ErrBadRequest)
}

// PostDiscord is the operator/home-server path (changelog, status). It
// skips the lane (Bagel's own posts are not perishable chat) but not the
// global bucket or the content bound: one misbehaving caller must not be
// able to trip the bot's global 429 for the whole fleet.
func (w *Worker) PostDiscord(ctx context.Context, channelID, content string) error {
	if w.discord == nil {
		return discord.ErrAuth
	}
	if channelID == "" || !discordContentOK(content) {
		return discord.ErrBadRequest
	}
	if err := w.takeDiscordGlobal(ctx); err != nil {
		return err
	}
	return w.discord.SendMessage(ctx, channelID, content, false)
}
