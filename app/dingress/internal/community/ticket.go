// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package community

import (
	"context"
	"strings"

	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func (b *Bot) onGuildCreate(ctx context.Context, raw []byte) error {
	ev, err := decode[struct {
		ID string `json:"id"`
	}](raw)
	if err != nil {
		return err
	}
	_, ok := b.bound(ctx, store.Guild{ID: ev.ID})
	if !ok {
		return nil
	}
	return nil
}

func (b *Bot) ensureDesk(ctx context.Context, cfg ddiscord.Config) {
	if !cfg.TicketsOn() {
		return
	}
	if cfg.TicketChannelID == "" {
		return
	}
	if b.Store == nil {
		return
	}
	if b.REST == nil {
		return
	}
	if !b.Store.ClaimDesk(ctx, store.Guild{ID: cfg.GuildID}) {
		return
	}
	_, _ = b.postDesk(ctx, cfg.TicketChannelID)
}

func (b *Bot) postDesk(ctx context.Context, channelID string) (discordapi.Message, error) {
	return b.REST.SendPanel(ctx, discordapi.EmbedPost{
		ChannelID: channelID,
		Embed:     ddiscord.TicketPanelEmbed(),
	}, discordapi.TicketDeskButtons())
}

func (b *Bot) postTicketPanel(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.TicketsOn() {
		return b.reply(ctx, in, "Tickets are off.")
	}
	channelID := cfg.TicketChannelID
	if channelID == "" {
		channelID = in.ChannelID
	}
	if err := b.Store.RememberDesk(ctx, store.Guild{ID: cfg.GuildID}); err != nil {
		return err
	}
	if _, err := b.postDesk(ctx, channelID); err != nil {
		return err
	}
	return b.reply(ctx, in, "Ticket panel posted.")
}

func (b *Bot) openTicket(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.TicketsOn() {
		return b.reply(ctx, in, "Tickets are off.")
	}
	ch, err := b.REST.CreateChannel(ctx, discordapi.GuildChannel{
		Guild: discordapi.Guild{ID: in.GuildID},
		Spec: discordapi.ChannelCreate{
			Name:                 ticketChannelName(in),
			Type:                 ddiscord.ChannelText,
			ParentID:             cfg.TicketCategoryID,
			PermissionOverwrites: ticketOverwrites(cfg, in),
		},
	})
	if err != nil {
		return err
	}
	_ = b.Store.TrackTicket(ctx, store.Ticket{ChannelID: ch.ID, GuildID: in.GuildID, OpenerID: in.Member.User.ID})
	if err := b.replyEmbed(ctx, in, ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{
		Opener: mention(in.Member.User) + " → <#" + ch.ID + ">",
	}), nil); err != nil {
		return err
	}
	_, err = b.REST.SendPanel(ctx, discordapi.EmbedPost{
		ChannelID: ch.ID,
		Content:   mention(in.Member.User),
		Embed:     ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{Opener: displayName(display{User: in.Member.User, Nick: in.Member.Nick})}),
	}, discordapi.TicketCloseButtons())
	return err
}

func ticketChannelName(in interactionEvent) string {
	name := "ticket-" + strings.ToLower(in.Member.User.Username)
	if name != "ticket-" {
		return name
	}
	return "ticket-" + in.Member.User.ID
}

func ticketOverwrites(cfg ddiscord.Config, in interactionEvent) []discordapi.PermissionOverwrite {
	overwrites := []discordapi.PermissionOverwrite{
		overwriteDeny(overwriteSpec{TargetID: in.GuildID, Kind: 0, Bits: permView}),
		overwriteAllow(overwriteSpec{TargetID: in.Member.User.ID, Kind: 1, Bits: permView | permSend}),
	}
	if cfg.ModsRoleID == "" {
		return overwrites
	}
	return append(overwrites, overwriteAllow(overwriteSpec{TargetID: cfg.ModsRoleID, Kind: 0, Bits: permView | permSend}))
}

func (b *Bot) closeTicket(ctx context.Context, in interactionEvent) error {
	t, ok := b.Store.Ticket(ctx, store.Channel{ID: in.ChannelID})
	if !ok {
		return b.reply(ctx, in, "This is not a ticket.")
	}
	if !canCloseTicket(t, in) {
		return b.reply(ctx, in, "Only the opener or a mod can close this.")
	}
	_ = b.Store.ForgetTicket(ctx, store.Channel{ID: t.ChannelID})
	_ = b.reply(ctx, in, "Closing.")
	return b.REST.DeleteChannel(ctx, discordapi.Snowflake{ID: t.ChannelID})
}

func canCloseTicket(t store.Ticket, in interactionEvent) bool {
	if t.OpenerID == in.Member.User.ID {
		return true
	}
	return canMod(permBits{Raw: in.Member.Permissions})
}
