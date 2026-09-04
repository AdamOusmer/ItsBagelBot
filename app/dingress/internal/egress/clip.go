// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package egress

import (
	"context"
	"strconv"
	"strings"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventdata "ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// HandleClipCreated decodes one data.twitch.clip.created message and posts
// the clip archive embed. See internal/domain/event/data/clip_events.go for
// why this rides BAGEL_DATA (a fact every interested consumer binds its own
// durable to) rather than a Discord command on a work queue. Always returns
// nil (ack): a malformed payload is dropped, and a Discord failure only
// logs -- the clip itself, and its chat reply, already happened on Twitch
// before this event was published.
func (w *Worker) HandleClipCreated(msg *bus.Message) error {
	var created eventdata.ClipCreated
	if err := codec.Unmarshal(msg.Payload, &created); err != nil {
		w.log.Warn("dropping clip-created event: malformed payload", zap.Error(err))
		return nil
	}
	w.announceDiscordClip(msg.Context(), clipJob{
		BroadcasterID: created.BroadcasterID,
		Embed: ddiscord.ClipEmbed(ddiscord.ClipCard{
			URL: created.URL, Clipper: created.Clipper, Title: created.Title,
		}),
	})
	return nil
}

// announceDiscordClip posts a clip archive embed, ported unchanged from
// outgress's worker.announceDiscordClip.
func (w *Worker) announceDiscordClip(ctx context.Context, job clipJob) {
	if w.discord == nil {
		return
	}
	if job.Embed.URL == "" {
		return
	}
	channelID, ok := w.clipsChannel(ctx, job)
	if !ok {
		return
	}
	if _, err := w.discord.SendEmbed(ctx, discapi.EmbedPost{ChannelID: channelID, Embed: job.Embed}); err != nil {
		w.log.Warn("discord clip embed failed",
			zap.String("broadcaster_id", job.BroadcasterID), zap.Error(err))
	}
}

// clipsChannel resolves where a clip archive post goes, if anywhere.
func (w *Worker) clipsChannel(ctx context.Context, job clipJob) (string, bool) {
	id, err := strconv.ParseUint(job.BroadcasterID, 10, 64)
	if err != nil {
		return "", false
	}
	cfg, enabled := w.discordConfig(ctx, discordUser{ID: id})
	if !enabled {
		return "", false
	}
	if !cfg.Connected() {
		return "", false
	}
	if !cfg.ClipsOn() {
		return "", false
	}
	channelID := strings.TrimSpace(cfg.ClipsChannelID)
	if channelID == "" {
		return "", false
	}
	return channelID, true
}
