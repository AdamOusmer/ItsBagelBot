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

// Message ports app/dingress/internal/community/message.go: the crumb
// level-up embed on a chat message, and the message-deleted/edited log
// lines. All three gateway types ride discord.ingress.event.message.
func Message(store discordstore.Store) module.Module {
	h := messageModule{store: store}
	b := module.NewModule("message")
	b.On("MESSAGE_CREATE", h.onCreate)
	b.On("MESSAGE_DELETE", h.onDelete)
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

func (h messageModule) onDelete(_ context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	return logLine(c, emit, logEntry{Title: "Message deleted", Body: messageLogBody(ev)})
}

func (h messageModule) onUpdate(_ context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MessageEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.Author.Bot {
		return nil
	}
	return logLine(c, emit, logEntry{Title: "Message edited", Body: messageLogBody(ev)})
}

func messageLogBody(ev decode.MessageEvent) string {
	body := "Message " + ev.ID + " in <#" + ev.ChannelID + ">"
	if ev.Content != "" {
		body += ": " + decode.Clip(ev.Content, 200)
	}
	return body
}
