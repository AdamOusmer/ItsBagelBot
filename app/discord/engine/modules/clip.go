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

// Clip ports app/dingress/internal/egress/clip.go's clip archive post. Unlike
// Live, this needs no outgress RPC: a clip post never needs to be edited
// later, so it is a plain TypePostEmbed Command like any other.
type Clip struct {
	Resolve ByBroadcaster
	Publish Publish
	Log     *zap.Logger
}

// HandleClipCreated decodes one data.twitch.clip.created message and emits
// the clip archive embed. Always returns nil (ack): a malformed payload is
// dropped, and a publish failure only logs -- the clip itself, and its chat
// reply, already happened on Twitch before this event was published.
func (c *Clip) HandleClipCreated(msg *bus.Message) error {
	var created eventdata.ClipCreated
	if err := codec.Unmarshal(msg.Payload, &created); err != nil {
		c.Log.Warn("dropping clip-created event: malformed payload", zap.Error(err))
		return nil
	}
	c.announce(msg.Context(), created)
	return nil
}

// announce posts the clip into every guild the broadcaster connected whose
// clips toggle is on. The embed is built once: it depends only on the clip.
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

// clipsChannel is one guild's clip archive channel, when it has one and the
// toggle is on.
func clipsChannel(cfg ddiscord.Config) (string, bool) {
	if !moduleGateOpen(cfg, cfg.ClipsOn()) {
		return "", false
	}
	channelID := strings.TrimSpace(cfg.ClipsChannelID)
	return channelID, channelID != ""
}
