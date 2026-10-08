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

func Message(store discordstore.Store) module.Module {
	h := messageModule{store: store}
	b := module.NewModule("message")
	b.On("MESSAGE_CREATE", h.onCreate)
	return b.Build()
}

type messageModule struct {
	store discordstore.Store
}

func (h messageModule) onCreate(ctx context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.GuildID == "" {
		return nil
	}
	h.remember(ctx, c, ev)
	if ev.Author.Bot {
		return nil
	}
	if !c.Config.LevelsOn() {
		return nil
	}
	_, leveled, level := h.store.AddXP(ctx, discordstore.Member{GuildID: ev.GuildID, UserID: ev.Author.ID})
	if !leveled {
		return nil
	}
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.Config.GuildID, ev.ChannelID),
		ddiscord.LevelUpEmbed(ddiscord.LevelUp{Who: decode.Mention(ev.Author), Level: level})))
	return nil
}

func (h messageModule) remember(ctx context.Context, c *module.Context, ev decode.MessageEvent) {
	if !c.Config.LogCategoryOn(ddiscord.LogMessages) {
		return
	}
	_ = h.store.RememberMessage(ctx, cacheEntry(ev))
}
