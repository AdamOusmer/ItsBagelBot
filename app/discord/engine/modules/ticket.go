// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"go.uber.org/zap"
)

// channelClient is the create/delete half of rpcclient.Client that
// ticket.go and voice.go both need. See
// internal/domain/rpc/discordoutgress's doc for why these are RPCs rather
// than Commands: a ticket or a voice clone's channel id does not exist
// until outgress's create call returns it, and both TrackTicket/TrackClone
// (below) and the immediate reply need that id.
type channelClient interface {
	CreateChannel(ctx context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error)
	DeleteChannel(ctx context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error)
}

// Ticket ports app/dingress/internal/community/ticket.go: the support desk
// panel, opening a private channel, and closing it.
func Ticket(store discordstore.Store, channels channelClient, log *zap.Logger) module.Module {
	h := ticketModule{store: store, channels: channels, log: log}
	b := module.NewModule("ticket")
	b.Slash("ticket", h.slash)
	b.Button(discordapi.CustomTicketOpen, h.open)
	b.Button(discordapi.CustomTicketClose, h.close)
	return b.Build()
}

type ticketModule struct {
	store    discordstore.Store
	channels channelClient
	log      *zap.Logger
}

// EnsureDesk claims (once per guild, via the store's Nx claim) and posts the
// persistent ticket desk panel. The dispatcher calls this on every resolved
// guild event, matching community's Bot.bound calling ensureDesk on every
// dispatch -- the Nx claim makes every call after the first a no-op.
func EnsureDesk(ctx context.Context, store discordstore.Store, cfg ddiscord.Config, emit module.Emit) {
	if !cfg.TicketsOn() || cfg.TicketChannelID == "" || store == nil {
		return
	}
	if !store.ClaimDesk(ctx, discordstore.Guild{ID: cfg.GuildID}) {
		return
	}
	emit(deskPanel(cfg.GuildID, cfg.TicketChannelID))
}

func deskPanel(guildID, channelID string) ddiscord.Command {
	return cmd.PostPanel(guildID, channelID, "", ddiscord.TicketPanelEmbed(), ticketDeskButtons())
}

func ticketDeskButtons() []ddiscord.ButtonSpec {
	return []ddiscord.ButtonSpec{{Style: discordapi.ButtonPrimary, Label: "Open a ticket", CustomID: discordapi.CustomTicketOpen}}
}

func ticketCloseButtons() []ddiscord.ButtonSpec {
	return []ddiscord.ButtonSpec{{Style: discordapi.ButtonDanger, Label: "Close ticket", CustomID: discordapi.CustomTicketClose}}
}

func (h ticketModule) slash(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	switch decode.FirstSub(in.Data.Options).Name {
	case "open":
		return h.open(ctx, c, emit)
	case "close":
		return h.close(ctx, c, emit)
	case "panel":
		return h.panel(ctx, c, in, emit)
	default:
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Use /ticket open, close, or panel.", true))
		return nil
	}
}

func (h ticketModule) panel(_ context.Context, c *module.Context, in decode.InteractionEvent, emit module.Emit) error {
	if !c.Config.TicketsOn() {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Tickets are off.", true))
		return nil
	}
	channelID := c.Config.TicketChannelID
	if channelID == "" {
		channelID = in.ChannelID
	}
	if err := h.store.RememberDesk(context.Background(), discordstore.Guild{ID: c.Config.GuildID}); err != nil {
		return err
	}
	emit(deskPanel(c.Config.GuildID, channelID))
	emit(cmd.Followup(c.Config.GuildID, in.Token, "Ticket panel posted.", true))
	return nil
}

func (h ticketModule) open(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !c.Config.TicketsOn() {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Tickets are off.", true))
		return nil
	}
	reply, err := h.channels.CreateChannel(ctx, discordoutgress.ChannelCreateRequest{
		GuildID: in.GuildID, Name: ticketChannelName(in), Type: ddiscord.ChannelText,
		ParentID: c.Config.TicketCategoryID, Overwrites: ticketOverwrites(c.Config, in),
	})
	if err != nil || reply.Error != "" {
		h.log.Warn("ticket channel create failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Could not open a ticket right now.", true))
		return nil
	}
	_ = h.store.TrackTicket(ctx, discordstore.Ticket{ChannelID: reply.ChannelID, GuildID: in.GuildID, OpenerID: in.Member.User.ID})

	emit(cmd.FollowupEmbed(c.Config.GuildID, in.Token, ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{
		Opener: decode.Mention(in.Member.User) + " → <#" + reply.ChannelID + ">",
	}), nil))
	opener := decode.DisplayName(decode.Display{User: in.Member.User, Nick: in.Member.Nick})
	emit(cmd.PostPanel(in.GuildID, reply.ChannelID, decode.Mention(in.Member.User),
		ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{Opener: opener}), ticketCloseButtons()))
	return nil
}

func ticketChannelName(in decode.InteractionEvent) string {
	name := "ticket-" + strings.ToLower(in.Member.User.Username)
	if name != "ticket-" {
		return name
	}
	return "ticket-" + in.Member.User.ID
}

func ticketOverwrites(cfg ddiscord.Config, in decode.InteractionEvent) []discordapi.PermissionOverwrite {
	overwrites := []discordapi.PermissionOverwrite{
		decode.OverwriteDeny(decode.OverwriteSpec{TargetID: in.GuildID, Kind: 0, Bits: decode.PermView}),
		decode.OverwriteAllow(decode.OverwriteSpec{TargetID: in.Member.User.ID, Kind: 1, Bits: decode.PermView | decode.PermSend}),
	}
	// Every staff tier, not just Mods: a ticket only Mods can read is
	// invisible to the Lead Mods and the Owner who are meant to escalate to.
	for _, roleID := range cfg.StaffRoleIDs() {
		overwrites = append(overwrites, decode.OverwriteAllow(decode.OverwriteSpec{
			TargetID: roleID, Kind: 0, Bits: decode.PermView | decode.PermSend,
		}))
	}
	return overwrites
}

func (h ticketModule) close(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	t, ok := h.store.Ticket(ctx, discordstore.Channel{ID: in.ChannelID})
	if !ok {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "This is not a ticket.", true))
		return nil
	}
	if !canCloseTicket(t, in) {
		emit(cmd.Followup(c.Config.GuildID, in.Token, "Only the opener or a mod can close this.", true))
		return nil
	}
	_ = h.store.ForgetTicket(ctx, discordstore.Channel{ID: t.ChannelID})
	emit(cmd.Followup(c.Config.GuildID, in.Token, "Closing.", true))
	reply, err := h.channels.DeleteChannel(ctx, discordoutgress.ChannelDeleteRequest{ChannelID: t.ChannelID})
	if err != nil || reply.Error != "" {
		h.log.Warn("ticket channel delete failed", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	return nil
}

func canCloseTicket(t discordstore.Ticket, in decode.InteractionEvent) bool {
	if t.OpenerID == in.Member.User.ID {
		return true
	}
	return decode.CanMod(in.Member.Permissions)
}
