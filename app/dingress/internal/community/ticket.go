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

func (b *Bot) postTicketPanel(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.TicketsOn() {
		return b.reply(ctx, in, "Tickets are off.")
	}
	channelID := cfg.TicketChannelID
	if channelID == "" {
		channelID = in.ChannelID
	}
	_, err := b.REST.SendPanel(ctx, discordapi.EmbedPost{
		ChannelID: channelID,
		Embed:     ddiscord.TicketPanelEmbed(),
	}, []discordapi.Button{{Style: 1, Label: "Open a ticket", CustomID: customTicketOpen}})
	if err != nil {
		return err
	}
	return b.reply(ctx, in, "Ticket panel posted.")
}

func (b *Bot) openTicket(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	if !cfg.TicketsOn() {
		return b.reply(ctx, in, "Tickets are off.")
	}
	name := "ticket-" + strings.ToLower(in.Member.User.Username)
	if name == "ticket-" {
		name = "ticket-" + in.Member.User.ID
	}
	overwrites := []discordapi.PermissionOverwrite{
		overwriteDeny(overwriteSpec{TargetID: in.GuildID, Kind: 0, Bits: permView}),
		overwriteAllow(overwriteSpec{TargetID: in.Member.User.ID, Kind: 1, Bits: permView | permSend}),
	}
	if cfg.ModsRoleID != "" {
		overwrites = append(overwrites, overwriteAllow(overwriteSpec{TargetID: cfg.ModsRoleID, Kind: 0, Bits: permView | permSend}))
	}
	ch, err := b.REST.CreateChannel(ctx, discordapi.GuildChannel{
		Guild: discordapi.Guild{ID: in.GuildID},
		Spec: discordapi.ChannelCreate{
			Name:                 name,
			Type:                 ddiscord.ChannelText,
			ParentID:             cfg.TicketCategoryID,
			PermissionOverwrites: overwrites,
		},
	})
	if err != nil {
		return err
	}
	_ = b.Store.TrackTicket(ctx, store.Ticket{ChannelID: ch.ID, GuildID: in.GuildID, OpenerID: in.Member.User.ID})
	if err := b.reply(ctx, in, "Ticket opened: <#"+ch.ID+">"); err != nil {
		return err
	}
	return b.REST.SendChat(ctx, discordapi.ChatPost{
		ChannelID: ch.ID,
		Content:   mention(in.Member.User) + " opened this ticket. Close it with /ticket close.",
	})
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

func (b *Bot) onTicketButton(ctx context.Context, cfg ddiscord.Config, in interactionEvent) error {
	h, ok := ticketButtons[in.Data.CustomID]
	if !ok {
		return nil
	}
	return h(b, ctx, cfg, in)
}

var ticketButtons = map[string]slashFn{
	customTicketOpen:  (*Bot).openTicket,
	customTicketClose: (*Bot).ticketCloseSlash,
}
