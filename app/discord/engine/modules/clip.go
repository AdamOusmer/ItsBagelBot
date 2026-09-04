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

func (c *Clip) announce(ctx context.Context, created eventdata.ClipCreated) {
	channelID, guildID, ok := c.clipsChannel(ctx, created.BroadcasterID)
	if !ok {
		return
	}
	embed := ddiscord.ClipEmbed(ddiscord.ClipCard{URL: created.URL, Clipper: created.Clipper, Title: created.Title})
	if embed.URL == "" {
		return
	}
	if err := c.Publish(ctx, cmd.PostEmbed(guildID, channelID, embed)); err != nil {
		c.Log.Warn("discord clip embed publish failed", zap.String("broadcaster_id", created.BroadcasterID), zap.Error(err))
	}
}

func (c *Clip) clipsChannel(ctx context.Context, broadcasterID string) (channelID, guildID string, ok bool) {
	id, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		return "", "", false
	}
	cfg, enabled := c.Resolve(ctx, id)
	if !enabled || !cfg.Connected() || !cfg.ClipsOn() {
		return "", "", false
	}
	channelID = strings.TrimSpace(cfg.ClipsChannelID)
	if channelID == "" {
		return "", "", false
	}
	return channelID, cfg.GuildID, true
}
