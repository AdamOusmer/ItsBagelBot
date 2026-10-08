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
	b.On("MESSAGE_UPDATE", h.onUpdate)
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
	if ev.Author.Bot || ev.GuildID == "" {
		return nil
	}
	h.remember(ctx, c, ev)
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

func (h messageModule) onUpdate(ctx context.Context, c *module.Context, _ module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.Author.ID == "" || ev.Author.Bot || ev.GuildID == "" || ev.Content == "" {
		return nil
	}
	h.remember(ctx, c, ev)
	return nil
}

func (h messageModule) remember(ctx context.Context, c *module.Context, ev decode.MessageEvent) {
	if !c.Config.LogCategoryOn(ddiscord.LogMessages) {
		return
	}
	attachments := make([]string, len(ev.Attachments))
	for i, a := range ev.Attachments {
		attachments[i] = a.URL
	}
	_ = h.store.RememberMessage(ctx, discordstore.CachedMessage{
		ID: ev.ID, GuildID: ev.GuildID, ChannelID: ev.ChannelID,
		AuthorID: ev.Author.ID, AuthorName: decode.DisplayName(decode.Display{User: ev.Author}),
		Content: ev.Content, Attachments: attachments,
	})
}
