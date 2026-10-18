// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func (h rankModule) onMessage(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.GuildID == "" || ev.Author.Bot {
		return nil
	}
	_, leveled, level := h.store.AddXP(ctx, discordstore.Member{GuildID: ev.GuildID, UserID: ev.Author.ID})
	if !leveled {
		return nil
	}
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.GuildID, ev.ChannelID),
		ddiscord.LevelUpEmbed(ddiscord.LevelUp{Who: decode.Mention(ev.Author), Level: level})))
	return nil
}

func (h logsModule) onMessageCreate(ctx context.Context, c *module.Context, _ module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	logs, err := settingsOf[ddiscord.LogsSettings](c)
	if err != nil {
		return err
	}
	if ev.GuildID == "" || !logs.CategoryOn(ddiscord.LogMessages) {
		return nil
	}
	_ = h.store.RememberMessage(ctx, cacheEntry(ev))
	return nil
}
