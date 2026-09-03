// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"

	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func (b *Bot) onMemberAdd(ctx context.Context, raw []byte) error {
	ev, err := decode[memberEvent](raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	cfg, ok := b.bound(ctx, ev.GuildID)
	if !ok {
		return nil
	}
	b.autorole(ctx, cfg, ev)
	if !cfg.WelcomeOn() {
		return b.logJoin(ctx, cfg, ev)
	}
	if cfg.WelcomeChannelID == "" {
		return b.logJoin(ctx, cfg, ev)
	}
	display := displayName(ev.User, ev.Nick)
	_, err = b.REST.SendEmbed(ctx, discordapi.EmbedPost{
		ChannelID: cfg.WelcomeChannelID,
		Embed:     ddiscord.WelcomeEmbed(display, avatarURL(ev.User)),
	})
	if err != nil {
		return err
	}
	return b.logJoin(ctx, cfg, ev)
}

func (b *Bot) autorole(ctx context.Context, cfg ddiscord.Config, ev memberEvent) {
	if cfg.MemberRoleID == "" {
		return
	}
	_ = b.REST.AddMemberRole(ctx, discordapi.MemberRole{
		GuildID: ev.GuildID, UserID: ev.User.ID, RoleID: cfg.MemberRoleID,
	})
}

func (b *Bot) onMemberRemove(ctx context.Context, raw []byte) error {
	ev, err := decode[memberEvent](raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	cfg, ok := b.bound(ctx, ev.GuildID)
	if !ok {
		return nil
	}
	display := displayName(ev.User, ev.Nick)
	if cfg.GoodbyeOn() && cfg.WelcomeChannelID != "" {
		_ = b.REST.SendMessage(ctx, cfg.WelcomeChannelID, ddiscord.GoodbyeContent(display), false)
	}
	return b.logLine(ctx, cfg, "Member left", display+" ("+ev.User.ID+")")
}

func (b *Bot) logJoin(ctx context.Context, cfg ddiscord.Config, ev memberEvent) error {
	return b.logLine(ctx, cfg, "Member joined", displayName(ev.User, ev.Nick)+" ("+ev.User.ID+")")
}

func (b *Bot) logLine(ctx context.Context, cfg ddiscord.Config, title, body string) error {
	if !cfg.LogsOn() {
		return nil
	}
	if cfg.LogChannelID == "" {
		return nil
	}
	_, err := b.REST.SendEmbed(ctx, discordapi.EmbedPost{
		ChannelID: cfg.LogChannelID,
		Embed:     ddiscord.LogEmbed(title, body),
	})
	return err
}
