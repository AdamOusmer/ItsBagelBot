// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"

	"ItsBagelBot/app/dingress/internal/store"
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
	cfg, ok := b.bound(ctx, store.Guild{ID: ev.GuildID})
	if !ok {
		return nil
	}
	b.autorole(ctx, cfg, ev)
	if !shouldWelcome(cfg) {
		return b.logJoin(ctx, cfg, ev)
	}
	shown := displayName(display{User: ev.User, Nick: ev.Nick})
	_, err = b.REST.SendEmbed(ctx, discordapi.EmbedPost{
		ChannelID: cfg.WelcomeChannelID,
		Embed:     ddiscord.WelcomeEmbed(shown, avatarURL(ev.User)),
	})
	if err != nil {
		return err
	}
	return b.logJoin(ctx, cfg, ev)
}

func shouldWelcome(cfg ddiscord.Config) bool {
	if !cfg.WelcomeOn() {
		return false
	}
	return cfg.WelcomeChannelID != ""
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
	cfg, ok := b.bound(ctx, store.Guild{ID: ev.GuildID})
	if !ok {
		return nil
	}
	shown := displayName(display{User: ev.User, Nick: ev.Nick})
	if shouldGoodbye(cfg) {
		_ = b.REST.SendChat(ctx, discordapi.ChatPost{
			ChannelID: cfg.WelcomeChannelID, Content: ddiscord.GoodbyeContent(shown),
		})
	}
	return b.logLine(ctx, cfg, logEntry{Title: "Member left", Body: shown + " (" + ev.User.ID + ")"})
}

func shouldGoodbye(cfg ddiscord.Config) bool {
	if !cfg.GoodbyeOn() {
		return false
	}
	return cfg.WelcomeChannelID != ""
}

func (b *Bot) logJoin(ctx context.Context, cfg ddiscord.Config, ev memberEvent) error {
	shown := displayName(display{User: ev.User, Nick: ev.Nick})
	return b.logLine(ctx, cfg, logEntry{Title: "Member joined", Body: shown + " (" + ev.User.ID + ")"})
}

type logEntry struct {
	Title string
	Body  string
}

func (b *Bot) logLine(ctx context.Context, cfg ddiscord.Config, entry logEntry) error {
	if !cfg.LogsOn() {
		return nil
	}
	if cfg.LogChannelID == "" {
		return nil
	}
	_, err := b.REST.SendEmbed(ctx, discordapi.EmbedPost{
		ChannelID: cfg.LogChannelID,
		Embed:     ddiscord.LogEmbed(entry.Title, entry.Body),
	})
	return err
}
