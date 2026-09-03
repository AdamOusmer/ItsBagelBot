// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"strconv"

	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
)

func (b *Bot) onMessage(ctx context.Context, raw []byte) error {
	ev, err := decode[messageEvent](raw)
	if err != nil {
		return err
	}
	if ev.Author.Bot {
		return nil
	}
	if ev.GuildID == "" {
		return nil
	}
	cfg, ok := b.bound(ctx, store.Guild{ID: ev.GuildID})
	if !ok {
		return nil
	}
	if !cfg.LevelsOn() {
		return nil
	}
	xp, leveled, level := b.Store.AddXP(ctx, store.Member{GuildID: ev.GuildID, UserID: ev.Author.ID})
	if !leveled {
		return nil
	}
	_ = xp
	return b.REST.SendChat(ctx, discordapi.ChatPost{
		ChannelID: ev.ChannelID,
		Content:   mention(ev.Author) + " reached level " + strconv.Itoa(level) + ".",
	})
}

func (b *Bot) onMessageDelete(ctx context.Context, raw []byte) error {
	ev, err := decode[messageEvent](raw)
	if err != nil {
		return err
	}
	cfg, ok := b.bound(ctx, store.Guild{ID: ev.GuildID})
	if !ok {
		return nil
	}
	body := "Message " + ev.ID + " in <#" + ev.ChannelID + ">"
	if ev.Content != "" {
		body += ": " + clip(ev.Content, 200)
	}
	return b.logLine(ctx, cfg, logEntry{Title: "Message deleted", Body: body})
}

func (b *Bot) onMessageUpdate(ctx context.Context, raw []byte) error {
	ev, err := decode[messageEvent](raw)
	if err != nil {
		return err
	}
	if ev.Author.Bot {
		return nil
	}
	cfg, ok := b.bound(ctx, store.Guild{ID: ev.GuildID})
	if !ok {
		return nil
	}
	body := "Message " + ev.ID + " in <#" + ev.ChannelID + ">"
	if ev.Content != "" {
		body += ": " + clip(ev.Content, 200)
	}
	return b.logLine(ctx, cfg, logEntry{Title: "Message edited", Body: body})
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
