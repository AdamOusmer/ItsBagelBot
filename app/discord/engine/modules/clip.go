// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	ddiscord "ItsBagelBot/internal/domain/discord"
	eventdata "ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type Clip struct {
	Resolve ByBroadcaster
	Publish Publish
	Log     *zap.Logger
}

func (c *Clip) HandleClipCreated(msg *bus.Message) error {
	var created eventdata.ClipCreated
	if err := codec.Unmarshal(msg.Payload, &created); err != nil {
		c.Log.Warn("dropping clip-created event: malformed payload", zap.Error(err))
		return nil
	}
	c.announce(msg.Context(), created)
	return nil
}

func (c *Clip) announce(ctx context.Context, created eventdata.ClipCreated) {
	id, err := strconv.ParseUint(created.BroadcasterID, 10, 64)
	if err != nil {
		return
	}
	embed := ddiscord.ClipEmbed(ddiscord.ClipCard{URL: created.URL, Clipper: created.Clipper, Title: created.Title})
	if embed.URL == "" {
		return
	}
	for _, guild := range c.Resolve(ctx, id) {
		channelID, ok := clipsChannel(guild.Config)
		if !ok {
			continue
		}
		if err := c.Publish(ctx, cmd.PostEmbed(cmd.ChannelTarget(guild.Guild.ID, channelID), embed)); err != nil {
			c.Log.Warn("discord clip embed publish failed",
				zap.String("broadcaster_id", created.BroadcasterID),
				zap.String("guild_id", guild.Guild.ID), zap.Error(err))
		}
	}
}

func clipsChannel(cfg ddiscord.Config) (string, bool) {
	if !moduleGateOpen(cfg, cfg.ClipsOn()) {
		return "", false
	}
	channelID := strings.TrimSpace(cfg.ClipsChannelID)
	return channelID, channelID != ""
}
