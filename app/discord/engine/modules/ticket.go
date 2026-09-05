// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
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
// voice.go needs. See internal/domain/rpc/discordoutgress's doc for why these
// are RPCs rather than Commands: a voice clone's channel id does not exist
// until outgress's create call returns it, and both TrackClone and the
// immediate reply need that id.
type channelClient interface {
	CreateChannel(ctx context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error)
	DeleteChannel(ctx context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error)
}

// ticketClient is the desk's half of rpcclient.Client: the three orchestrations
// outgress performs on the engine's behalf, plus the channel delete the open
// path rolls back with.
type ticketClient interface {
	TicketOpen(ctx context.Context, req discordoutgress.TicketOpenRequest) (discordoutgress.TicketOpenReply, error)
	TicketClaim(ctx context.Context, req discordoutgress.TicketClaimRequest) (discordoutgress.TicketClaimReply, error)
	TicketClose(ctx context.Context, req discordoutgress.TicketCloseRequest) (discordoutgress.TicketCloseReply, error)
	TicketAddMember(ctx context.Context, req discordoutgress.TicketMemberAddRequest) (discordoutgress.TicketMemberAddReply, error)
	DeleteChannel(ctx context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error)
}

// Ticket is the support desk: the persistent panel, opening a private channel,
// claiming it, adding people to it, and closing it with a transcript.
//
// Every command this module emits rides the DEFAULT lane (ddiscord.Lane sends
// anything that is not a ModType there). That is deliberate: a ticket is a
// support conversation, and putting desk traffic on the moderation lane would
// let one guild's transcript upload sit in front of another guild's ban.
func Ticket(store discordstore.Store, tickets ticketClient, log *zap.Logger) module.Module {
	h := ticketModule{store: store, tickets: tickets, log: log}
	b := module.NewModule("ticket")
	b.Slash("ticket", h.slash)
	b.Button(discordapi.CustomTicketOpen, h.open)
	b.Button(discordapi.CustomTicketClaim, h.claim)
	b.Button(discordapi.CustomTicketClose, h.close)
	return b.Build()
}

type ticketModule struct {
	store   discordstore.Store
	tickets ticketClient
	log     *zap.Logger
}

// deskUnavailable reports whether EnsureDesk has nothing to claim: tickets
// are off, no channel is configured to host the desk, or there is no store
// to claim it in (a nil store is possible in tests that exercise other
// modules without wiring one).
func deskUnavailable(store discordstore.Store, cfg ddiscord.Config) bool {
	return !cfg.TicketsOn() || cfg.TicketChannelID == "" || store == nil
}

// EnsureDesk claims (once per guild, via the store's Nx claim) and posts the
// persistent ticket desk panel. The dispatcher calls this on every resolved
// guild event; the Nx claim makes every call after the first a no-op.
func EnsureDesk(ctx context.Context, store discordstore.Store, cfg ddiscord.Config, emit module.Emit) {
	if deskUnavailable(store, cfg) {
		return
	}
	if !store.ClaimDesk(ctx, discordstore.Guild{ID: cfg.GuildID}) {
		return
	}
	emit(deskPanel(cfg))
}

func deskPanel(cfg ddiscord.Config) ddiscord.Command {
	spec := cfg.TicketPanel()
	return cmd.PostPanel(cmd.ChannelTarget(cfg.GuildID, cfg.TicketChannelID), "",
		ddiscord.TicketPanelEmbed(spec), ticketDeskButtons(spec))
}

func ticketDeskButtons(spec ddiscord.TicketPanelSpec) []ddiscord.ButtonSpec {
	return []ddiscord.ButtonSpec{{Style: discordapi.ButtonPrimary, Label: spec.Button, CustomID: discordapi.CustomTicketOpen}}
}

// ticketOpenButtons are the two controls inside a ticket channel.
func ticketOpenButtons() []ddiscord.ButtonSpec {
	return []ddiscord.ButtonSpec{
		{Style: discordapi.ButtonSecondary, Label: "Claim", CustomID: discordapi.CustomTicketClaim},
		{Style: discordapi.ButtonDanger, Label: "Close ticket", CustomID: discordapi.CustomTicketClose},
	}
}

func (h ticketModule) slash(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	switch sub := decode.FirstSub(in.Data.Options); sub.Name {
	case "open":
		return h.open(ctx, c, emit)
	case "close":
		return h.close(ctx, c, emit)
	case "claim":
		return h.claim(ctx, c, emit)
	case "add":
		return h.add(ctx, c, in, sub, emit)
	case "panel":
		return h.panel(ctx, c, in, emit)
	default:
		h.reply(c, in, emit, "Use /ticket open, close, claim, add, or panel.")
		return nil
	}
}

// reply is the one shape every desk outcome takes: an ephemeral followup on
// the interaction that triggered it. Every path answers, including the
// refusals -- a button that appears to do nothing is the worst outcome here.
func (h ticketModule) reply(c *module.Context, in decode.InteractionEvent, emit module.Emit, text string) {
	emit(cmd.Followup(cmd.GuildTarget(c.Config.GuildID), cmd.Token(in.Token), text, true))
}

func (h ticketModule) panel(ctx context.Context, c *module.Context, in decode.InteractionEvent, emit module.Emit) error {
	if !c.Config.TicketsOn() {
		h.reply(c, in, emit, "Tickets are off.")
		return nil
	}
	cfg := c.Config
	if cfg.TicketChannelID == "" {
		cfg.TicketChannelID = in.ChannelID
	}
	err := h.store.RememberDesk(ctx, discordstore.DeskPanel{GuildID: cfg.GuildID, ChannelID: cfg.TicketChannelID})
	if err != nil {
		return err
	}
	emit(deskPanel(cfg))
	h.reply(c, in, emit, "Ticket panel posted.")
	return nil
}

func (h ticketModule) open(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if !c.Config.TicketsOn() {
		h.reply(c, in, emit, "Tickets are off.")
		return nil
	}
	limit := c.Config.TicketOpenLimitN()
	held := h.store.OpenTicketCount(ctx, discordstore.Member{GuildID: in.GuildID, UserID: in.Member.User.ID})
	if held >= limit {
		h.reply(c, in, emit, atLimitText(held))
		return nil
	}
	return h.createTicket(ctx, c, in, held, emit)
}

// atLimitText names the number back to the opener rather than saying "too
// many": a streamer who set the cap to 3 gets a message that matches what they
// configured.
func atLimitText(held int) string {
	if held == 1 {
		return "You already have 1 open ticket."
	}
	return "You already have " + strconv.Itoa(held) + " open tickets."
}

func (h ticketModule) createTicket(ctx context.Context, c *module.Context, in decode.InteractionEvent, held int, emit module.Emit) error {
	opener := decode.DisplayName(decode.Display{User: in.Member.User, Nick: in.Member.Nick})
	reply, err := h.tickets.TicketOpen(ctx, discordoutgress.TicketOpenRequest{
		GuildID: in.GuildID, Name: ticketChannelName(in, held+1), ParentID: c.Config.TicketCategoryID,
		Overwrites: ticketOverwrites(c.Config, in), Content: decode.Mention(in.Member.User),
		Embed:   ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{Opener: opener}),
		Buttons: ticketOpenButtons(),
	})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket open failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		h.rollback(ctx, reply.ChannelID)
		h.reply(c, in, emit, "Could not open a ticket right now.")
		return nil
	}
	return h.recordTicket(ctx, c, in, reply, emit)
}

func (h ticketModule) recordTicket(ctx context.Context, c *module.Context, in decode.InteractionEvent, reply discordoutgress.TicketOpenReply, emit module.Emit) error {
	got, err := h.store.TrackTicket(ctx, discordstore.TicketOpen{
		GuildID: in.GuildID, ChannelID: reply.ChannelID, OpenerID: in.Member.User.ID,
		PanelMessageID: reply.MessageID, OpenLimit: c.Config.TicketOpenLimitN(),
	})
	if err != nil {
		// The channel exists but no row points at it: delete it rather than
		// leave a ticket the desk can never close, claim or transcribe.
		h.log.Error("ticket row not recorded; rolling the channel back",
			zap.String("channel_id", reply.ChannelID), zap.Error(err))
		h.rollback(ctx, reply.ChannelID)
		h.reply(c, in, emit, "Could not open a ticket right now.")
		return nil
	}
	if got.AtLimit {
		// The pre-check passed and the insert still refused: two presses
		// raced. Same rollback, and the opener is told the real number.
		h.rollback(ctx, reply.ChannelID)
		h.reply(c, in, emit, atLimitText(got.OpenCount))
		return nil
	}
	h.reply(c, in, emit, "Ticket opened: <#"+reply.ChannelID+">")
	return nil
}

// rollback deletes a channel whose ticket row does not exist.
func (h ticketModule) rollback(ctx context.Context, channelID string) {
	if channelID == "" {
		return
	}
	reply, err := h.tickets.DeleteChannel(ctx, discordoutgress.ChannelDeleteRequest{ChannelID: channelID})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket rollback failed", zap.String("channel_id", channelID), zap.Error(err))
	}
}

// ticketChannelName is ticket-<username>-<n>. The number is the opener's nth
// live ticket, not a global counter: it exists so a member allowed two open
// tickets can tell them apart in the sidebar, and Discord does not require
// channel names to be unique.
func ticketChannelName(in decode.InteractionEvent, n int) string {
	name := strings.ToLower(in.Member.User.Username)
	if name == "" {
		name = in.Member.User.ID
	}
	return "ticket-" + name + "-" + strconv.Itoa(n)
}

// ticketOverwrites is the private channel's permission set: nobody but the
// opener and the desk staff.
func ticketOverwrites(cfg ddiscord.Config, in decode.InteractionEvent) []discordapi.PermissionOverwrite {
	overwrites := []discordapi.PermissionOverwrite{
		decode.OverwriteDeny(decode.OverwriteSpec{TargetID: in.GuildID, Kind: 0, Bits: decode.PermView}),
		decode.OverwriteAllow(decode.OverwriteSpec{
			TargetID: in.Member.User.ID, Kind: 1,
			Bits: decode.PermView | decode.PermSend | decode.PermReadHistory,
		}),
	}
	// Every staff tier, not just Mods: a ticket only Mods can read is
	// invisible to the Lead Mods and the Owner who are meant to escalate to.
	for _, roleID := range cfg.TicketStaffRoleIDs() {
		overwrites = append(overwrites, decode.OverwriteAllow(decode.OverwriteSpec{
			TargetID: roleID, Kind: 0,
			Bits: decode.PermView | decode.PermSend | decode.PermReadHistory | decode.PermManageMessages,
		}))
	}
	return overwrites
}

func (h ticketModule) claim(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	t, ok := h.store.Ticket(ctx, discordstore.Channel{ID: in.ChannelID})
	if !ok {
		h.reply(c, in, emit, "This is not a ticket.")
		return nil
	}
	if !isTicketStaffOrMod(c.Config, in) {
		h.reply(c, in, emit, "Only ticket staff can claim this.")
		return nil
	}
	if t.ClaimedBy != "" {
		h.reply(c, in, emit, "This ticket is already claimed by <@"+t.ClaimedBy+">.")
		return nil
	}
	return h.recordClaim(ctx, c, in, t, emit)
}

func (h ticketModule) recordClaim(ctx context.Context, c *module.Context, in decode.InteractionEvent, t discordstore.Ticket, emit module.Emit) error {
	claim := discordstore.TicketClaim{GuildID: t.GuildID, ChannelID: t.ChannelID, StaffID: in.Member.User.ID}
	if err := h.store.ClaimTicket(ctx, claim); err != nil {
		h.log.Warn("ticket claim not recorded", zap.String("channel_id", t.ChannelID), zap.Error(err))
		h.reply(c, in, emit, "Could not claim this ticket right now.")
		return nil
	}
	staff := decode.DisplayName(decode.Display{User: in.Member.User, Nick: in.Member.Nick})
	reply, err := h.tickets.TicketClaim(ctx, discordoutgress.TicketClaimRequest{
		ChannelID: t.ChannelID, MessageID: t.PanelMessageID, Content: "<@" + t.OpenerID + ">",
		Embed: ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{
			Opener: "<@" + t.OpenerID + ">", ClaimedBy: staff,
		}),
		Note: staff + " claimed this ticket.",
	})
	if rpcFailed(err, reply.Error) {
		// The claim is recorded; only the card is stale. Say so rather than
		// implying the claim failed.
		h.log.Warn("ticket claim card not updated", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	h.reply(c, in, emit, "Claimed.")
	return nil
}

func (h ticketModule) close(ctx context.Context, c *module.Context, emit module.Emit) error {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	t, ok := h.store.Ticket(ctx, discordstore.Channel{ID: in.ChannelID})
	if !ok {
		h.reply(c, in, emit, "This is not a ticket.")
		return nil
	}
	if !canCloseTicket(t, in, c.Config) {
		h.reply(c, in, emit, "Only the opener or ticket staff can close this.")
		return nil
	}
	return h.finishClose(ctx, c, in, t, emit)
}

func (h ticketModule) finishClose(ctx context.Context, c *module.Context, in decode.InteractionEvent, t discordstore.Ticket, emit module.Emit) error {
	reply, err := h.tickets.TicketClose(ctx, h.closeRequest(c.Config, in, t))
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket close failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		h.reply(c, in, emit, "Could not close this ticket right now.")
		return nil
	}
	h.storeClose(ctx, in, t, reply)
	h.reply(c, in, emit, "Ticket closed.")
	return nil
}

func (h ticketModule) closeRequest(cfg ddiscord.Config, in decode.InteractionEvent, t discordstore.Ticket) discordoutgress.TicketCloseRequest {
	return discordoutgress.TicketCloseRequest{
		GuildID: t.GuildID, ChannelID: t.ChannelID, ChannelName: in.Channel.Name,
		OpenerID: t.OpenerID, Transcript: cfg.TicketTranscriptOn(),
		LogChannelID: cfg.TicketLogChannel(), ArchiveCategoryID: cfg.TicketArchiveCategory(),
		StaffRoleIDs: cfg.TicketStaffRoleIDs(),
		Summary: discordoutgress.TicketCloseSummary{
			Opener: "<@" + t.OpenerID + ">",
			Closer: decode.DisplayName(decode.Display{User: in.Member.User, Nick: in.Member.Nick}),
		},
	}
}

// storeClose records the terminal state and the transcript. Both writes are
// best effort at this point: Discord has already archived or deleted the
// channel, and failing the interaction now would tell the user the close did
// not happen when it did.
func (h ticketModule) storeClose(ctx context.Context, in decode.InteractionEvent, t discordstore.Ticket, reply discordoutgress.TicketCloseReply) {
	err := h.store.CloseTicket(ctx, discordstore.TicketClose{
		GuildID: t.GuildID, ChannelID: t.ChannelID, ClosedBy: in.Member.User.ID,
		ArchivedChannelID: reply.ArchivedChannelID,
	})
	if err != nil {
		h.log.Error("ticket close not recorded", zap.String("channel_id", t.ChannelID), zap.Error(err))
	}
	if reply.TranscriptBody == "" {
		return
	}
	err = h.store.PutTranscript(ctx, discordstore.Transcript{
		TicketID: t.ID, Body: reply.TranscriptBody, MessageCount: reply.MessageCount,
	})
	if err != nil {
		h.log.Error("ticket transcript not stored", zap.Int("ticket_id", t.ID), zap.Error(err))
	}
}

func canCloseTicket(t discordstore.Ticket, in decode.InteractionEvent, cfg ddiscord.Config) bool {
	if t.OpenerID == in.Member.User.ID {
		return true
	}
	return isTicketStaffOrMod(cfg, in)
}

// add grants one member access to this ticket (/ticket add user:<@user>).
func (h ticketModule) add(ctx context.Context, c *module.Context, in decode.InteractionEvent, sub decode.InteractionOption, emit module.Emit) error {
	t, ok := h.store.Ticket(ctx, discordstore.Channel{ID: in.ChannelID})
	if !ok {
		h.reply(c, in, emit, "This is not a ticket.")
		return nil
	}
	if !canCloseTicket(t, in, c.Config) {
		h.reply(c, in, emit, "Only the opener or ticket staff can add someone.")
		return nil
	}
	userID := decode.OptionUser(sub.Options, "user")
	if userID == "" {
		h.reply(c, in, emit, "Name the member to add.")
		return nil
	}
	reply, err := h.tickets.TicketAddMember(ctx, discordoutgress.TicketMemberAddRequest{ChannelID: t.ChannelID, UserID: userID})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket add failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		h.reply(c, in, emit, "Could not add them right now.")
		return nil
	}
	h.reply(c, in, emit, "Added <@"+userID+"> to this ticket.")
	return nil
}
