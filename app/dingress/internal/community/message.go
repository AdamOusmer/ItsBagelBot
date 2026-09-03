// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"strconv"
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
	cfg, ok := b.bound(ctx, ev.GuildID)
	if !ok {
		return nil
	}
	if !cfg.LevelsOn() {
		return nil
	}
	xp, leveled, level := b.Store.AddXP(ctx, ev.GuildID, ev.Author.ID)
	if !leveled {
		return nil
	}
	_ = xp
	return b.REST.SendMessage(ctx, ev.ChannelID, mention(ev.Author.ID)+" reached level "+strconv.Itoa(level)+".", false)
}

func (b *Bot) onMessageDelete(ctx context.Context, raw []byte) error {
	ev, err := decode[messageEvent](raw)
	if err != nil {
		return err
	}
	cfg, ok := b.bound(ctx, ev.GuildID)
	if !ok {
		return nil
	}
	body := "Message " + ev.ID + " in <#" + ev.ChannelID + ">"
	if ev.Content != "" {
		body += ": " + clip(ev.Content, 200)
	}
	return b.logLine(ctx, cfg, "Message deleted", body)
}

func (b *Bot) onMessageUpdate(ctx context.Context, raw []byte) error {
	ev, err := decode[messageEvent](raw)
	if err != nil {
		return err
	}
	if ev.Author.Bot {
		return nil
	}
	cfg, ok := b.bound(ctx, ev.GuildID)
	if !ok {
		return nil
	}
	body := "Message " + ev.ID + " in <#" + ev.ChannelID + ">"
	if ev.Content != "" {
		body += ": " + clip(ev.Content, 200)
	}
	return b.logLine(ctx, cfg, "Message edited", body)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
