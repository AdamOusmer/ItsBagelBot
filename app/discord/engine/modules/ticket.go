// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"go.uber.org/zap"
)

type channelClient interface {
	CreateChannel(ctx context.Context, req discordoutgress.ChannelCreateRequest) (discordoutgress.ChannelCreateReply, error)
	DeleteChannel(ctx context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error)
}

type ticketClient interface {
	TicketOpen(ctx context.Context, req discordoutgress.TicketOpenRequest) (discordoutgress.TicketOpenReply, error)
	TicketClaim(ctx context.Context, req discordoutgress.TicketClaimRequest) (discordoutgress.TicketClaimReply, error)
	TicketClose(ctx context.Context, req discordoutgress.TicketCloseRequest) (discordoutgress.TicketCloseReply, error)
	TicketAddMember(ctx context.Context, req discordoutgress.TicketMemberAddRequest) (discordoutgress.TicketMemberAddReply, error)
	TicketPanel(ctx context.Context, req discordoutgress.TicketPanelRequest) (discordoutgress.TicketPanelReply, error)
	ModifyChannel(ctx context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error)
	DeleteChannel(ctx context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error)
}

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

func deskUnavailable(store discordstore.Store, cfg ddiscord.Config) bool {
	return !cfg.TicketsOn() || cfg.TicketChannelID == "" || store == nil
}

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

func ticketOpenButtons() []ddiscord.ButtonSpec {
	return []ddiscord.ButtonSpec{
		{Style: discordapi.ButtonSecondary, Label: "Claim", CustomID: discordapi.CustomTicketClaim},
		{Style: discordapi.ButtonDanger, Label: "Close ticket", CustomID: discordapi.CustomTicketClose},
	}
}

type deskCall struct {
	mod  *module.Context
	in   decode.InteractionEvent
	emit module.Emit
}

func deskCallFrom(c *module.Context, emit module.Emit) (deskCall, error) {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return deskCall{}, err
	}
	return deskCall{mod: c, in: in, emit: emit}, nil
}

func (d deskCall) reply(text string) {
	d.emit(cmd.Followup(cmd.GuildTarget(d.mod.Config.GuildID), cmd.Token(d.in.Token), text, true))
}

func (h ticketModule) slash(ctx context.Context, c *module.Context, emit module.Emit) error {
	call, err := deskCallFrom(c, emit)
	if err != nil {
		return err
	}
	switch sub := decode.FirstSub(call.in.Data.Options); sub.Name {
	case "open":
		return h.open(ctx, c, emit)
	case "close":
		return h.close(ctx, c, emit)
	case "claim":
		return h.claim(ctx, c, emit)
	case "add":
		return h.add(ctx, call, sub)
	case "panel":
		return h.panel(ctx, call)
	default:
		call.reply("Use /ticket open, close, claim, add, or panel.")
		return nil
	}
}

func (h ticketModule) panel(ctx context.Context, call deskCall) error {
	if !call.mod.Config.TicketsOn() {
		call.reply("Tickets are off.")
		return nil
	}
	if !isTicketStaffOrMod(call.mod.Config, call.in) {
		call.reply("Only ticket staff can post the panel.")
		return nil
	}
	cfg := call.mod.Config
	if cfg.TicketChannelID == "" {
		cfg.TicketChannelID = call.in.ChannelID
	}
	return h.postPanel(ctx, call, cfg)
}

func (h ticketModule) postPanel(ctx context.Context, call deskCall, cfg ddiscord.Config) error {
	spec := cfg.TicketPanel()
	reply, err := h.tickets.TicketPanel(ctx, discordoutgress.TicketPanelRequest{
		GuildID: cfg.GuildID, ChannelID: cfg.TicketChannelID,
		Embed: ddiscord.TicketPanelEmbed(spec), Buttons: ticketDeskButtons(spec),
	})
	if rpcFailed(err, reply.Error) || reply.MessageID == "" {
		h.log.Warn("ticket panel post failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		call.reply("Could not post the ticket panel right now.")
		return nil
	}
	remembered := discordstore.DeskPanel{
		GuildID: cfg.GuildID, ChannelID: cfg.TicketChannelID, MessageID: reply.MessageID,
	}
	if err := h.store.RememberDesk(ctx, remembered); err != nil {
		h.log.Error("ticket desk pointer not stored",
			zap.String("guild_id", cfg.GuildID), zap.String("message_id", reply.MessageID), zap.Error(err))
	}
	call.reply("Ticket panel posted.")
	return nil
}

func (h ticketModule) open(ctx context.Context, c *module.Context, emit module.Emit) error {
	call, err := deskCallFrom(c, emit)
	if err != nil {
		return err
	}
	if !call.mod.Config.TicketsOn() {
		call.reply("Tickets are off.")
		return nil
	}
	if !h.store.TicketsDurable(ctx) {
		h.log.Error("ticket open refused: no durable ticket store",
			zap.String("guild_id", call.in.GuildID), zap.String("user_id", call.in.Member.User.ID))
		call.reply("The ticket desk is unavailable right now. Try again shortly.")
		return nil
	}
	limit := call.mod.Config.TicketOpenLimitN()
	held := h.store.OpenTicketCount(ctx, discordstore.Member{GuildID: call.in.GuildID, UserID: call.in.Member.User.ID})
	if held >= limit {
		call.reply(atLimitText(held))
		return nil
	}
	return h.createTicket(ctx, call)
}

func atLimitText(held int) string {
	if held == 1 {
		return "You already have 1 open ticket."
	}
	return "You already have " + strconv.Itoa(held) + " open tickets."
}

func (h ticketModule) createTicket(ctx context.Context, call deskCall) error {
	in, cfg := call.in, call.mod.Config
	opener := decode.DisplayName(decode.Display{User: in.Member.User, Nick: in.Member.Nick})
	reply, err := h.tickets.TicketOpen(ctx, discordoutgress.TicketOpenRequest{
		GuildID: in.GuildID, Name: ddiscord.TicketChannelName(ticketNameBase(in), 0), ParentID: cfg.TicketCategoryID,
		Overwrites: ticketOverwrites(cfg, in), Content: decode.Mention(in.Member.User),
		Embed:   ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{Opener: opener}),
		Buttons: ticketOpenButtons(),
	})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket open failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		h.deleteOrphanChannel(ctx, reply.ChannelID)
		call.reply("Could not open a ticket right now.")
		return nil
	}
	return h.recordTicket(ctx, call, reply)
}

func (h ticketModule) recordTicket(ctx context.Context, call deskCall, reply discordoutgress.TicketOpenReply) error {
	in := call.in
	got, err := h.store.TrackTicket(ctx, discordstore.TicketOpen{
		GuildID: in.GuildID, ChannelID: reply.ChannelID, OpenerID: in.Member.User.ID,
		PanelMessageID: reply.MessageID, OpenLimit: call.mod.Config.TicketOpenLimitN(),
	})
	if err != nil {
		h.log.Error("ticket row not recorded; rolling the channel back",
			zap.String("channel_id", reply.ChannelID), zap.Error(err))
		h.deleteOrphanChannel(ctx, reply.ChannelID)
		call.reply("Could not open a ticket right now.")
		return nil
	}
	if got.AtLimit {
		h.deleteOrphanChannel(ctx, reply.ChannelID)
		call.reply(atLimitText(got.OpenCount))
		return nil
	}
	h.nameTicket(ctx, in, reply.ChannelID, got.TicketID)
	call.reply("Ticket opened: <#" + reply.ChannelID + ">")
	return nil
}

func (h ticketModule) nameTicket(ctx context.Context, in decode.InteractionEvent, channelID string, ticketID int) {
	if channelID == "" || ticketID <= 0 {
		return
	}
	name := ddiscord.TicketChannelName(ticketNameBase(in), ticketID)
	reply, err := h.tickets.ModifyChannel(ctx, discordoutgress.ChannelModifyRequest{ChannelID: channelID, Name: name})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket channel not renamed",
			zap.String("channel_id", channelID), zap.String("name", name), zap.Error(err))
	}
}

func (h ticketModule) deleteOrphanChannel(ctx context.Context, channelID string) {
	if channelID == "" {
		return
	}
	reply, err := h.tickets.DeleteChannel(ctx, discordoutgress.ChannelDeleteRequest{ChannelID: channelID})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket delete orphan channel failed", zap.String("channel_id", channelID), zap.Error(err))
	}
}

func ticketNameBase(in decode.InteractionEvent) string {
	if name := ddiscord.SanitizeChannelName(in.Member.User.Username); name != "" {
		return name
	}
	return in.Member.User.ID
}

func ticketOverwrites(cfg ddiscord.Config, in decode.InteractionEvent) []discordapi.PermissionOverwrite {
	overwrites := []discordapi.PermissionOverwrite{
		decode.OverwriteDeny(decode.OverwriteSpec{TargetID: in.GuildID, Kind: 0, Bits: decode.PermView}),
		decode.OverwriteAllow(decode.OverwriteSpec{
			TargetID: in.Member.User.ID, Kind: 1,
			Bits: decode.PermView | decode.PermSend | decode.PermReadHistory,
		}),
	}
	for _, roleID := range cfg.TicketStaffRoleIDs() {
		overwrites = append(overwrites, decode.OverwriteAllow(decode.OverwriteSpec{
			TargetID: roleID, Kind: 0,
			Bits: decode.PermView | decode.PermSend | decode.PermReadHistory | decode.PermManageMessages,
		}))
	}
	return overwrites
}

// Keep the read scoped to the interaction's guild, or one guild can act on another's ticket.
func (h ticketModule) ticketFor(ctx context.Context, call deskCall) (discordstore.Ticket, bool) {
	ch := discordstore.Channel{ID: call.in.ChannelID}
	h.retryPendingClose(ctx, ch)
	t, ok := h.store.Ticket(ctx, discordstore.Guild{ID: call.in.GuildID}, ch)
	if !ok {
		call.reply("This is not a ticket.")
		return discordstore.Ticket{}, false
	}
	return t, true
}

func (h ticketModule) retryPendingClose(ctx context.Context, ch discordstore.Channel) {
	pending, ok := h.store.PendingClose(ctx, ch)
	if !ok {
		return
	}
	if err := h.store.CloseTicket(ctx, pending); err != nil {
		h.log.Error("pending ticket close still not recorded",
			zap.String("channel_id", ch.ID), zap.Error(err))
		return
	}
	if err := h.store.ClearPendingClose(ctx, ch); err != nil {
		h.log.Warn("pending close marker not cleared", zap.String("channel_id", ch.ID), zap.Error(err))
	}
}

func (h ticketModule) claim(ctx context.Context, c *module.Context, emit module.Emit) error {
	call, err := deskCallFrom(c, emit)
	if err != nil {
		return err
	}
	t, ok := h.ticketFor(ctx, call)
	if !ok {
		return nil
	}
	if !isTicketStaffOrMod(call.mod.Config, call.in) {
		call.reply("Only ticket staff can claim this.")
		return nil
	}
	if t.ClaimedBy != "" {
		call.reply("This ticket is already claimed by <@" + t.ClaimedBy + ">.")
		return nil
	}
	return h.recordClaim(ctx, call, t)
}

func (h ticketModule) recordClaim(ctx context.Context, call deskCall, t discordstore.Ticket) error {
	in := call.in
	claim := discordstore.TicketClaim{GuildID: t.GuildID, ChannelID: t.ChannelID, StaffID: in.Member.User.ID}
	if err := h.store.ClaimTicket(ctx, claim); err != nil {
		h.log.Warn("ticket claim not recorded", zap.String("channel_id", t.ChannelID), zap.Error(err))
		call.reply("Could not claim this ticket right now.")
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
		h.log.Warn("ticket claim card not updated", zap.Error(err), zap.String("outgress_error", reply.Error))
	}
	call.reply("Claimed.")
	return nil
}

func (h ticketModule) close(ctx context.Context, c *module.Context, emit module.Emit) error {
	call, err := deskCallFrom(c, emit)
	if err != nil {
		return err
	}
	t, ok := h.ticketFor(ctx, call)
	if !ok {
		return nil
	}
	if discordstore.TicketOver(t.Status) {
		call.reply("This ticket is already closed.")
		return nil
	}
	if !canCloseTicket(t, call.in, call.mod.Config) {
		call.reply("Only the opener or ticket staff can close this.")
		return nil
	}
	return h.finishClose(ctx, call, t)
}

func (h ticketModule) finishClose(ctx context.Context, call deskCall, t discordstore.Ticket) error {
	reply, err := h.tickets.TicketClose(ctx, h.closeRequest(call.mod.Config, call.in, t))
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket close failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		call.reply("Could not close this ticket right now.")
		return nil
	}
	h.storeClose(ctx, call.in, t, reply)
	call.reply("Ticket closed.")
	return nil
}

func (h ticketModule) closeRequest(cfg ddiscord.Config, in decode.InteractionEvent, t discordstore.Ticket) discordoutgress.TicketCloseRequest {
	return discordoutgress.TicketCloseRequest{
		GuildID: t.GuildID, ChannelID: t.ChannelID, ChannelName: in.Channel.Name, TicketID: t.ID,
		OpenerID: t.OpenerID, Transcript: cfg.TicketTranscriptOn(),
		LogChannelID: cfg.TicketLogChannel(), ArchiveCategoryID: cfg.TicketArchiveCategory(),
		StaffRoleIDs: cfg.TicketStaffRoleIDs(),
		Summary: discordoutgress.TicketCloseSummary{
			Opener: "<@" + t.OpenerID + ">",
			Closer: decode.DisplayName(decode.Display{User: in.Member.User, Nick: in.Member.Nick}),
		},
	}
}

func (h ticketModule) storeClose(ctx context.Context, in decode.InteractionEvent, t discordstore.Ticket, reply discordoutgress.TicketCloseReply) {
	done := discordstore.TicketClose{
		GuildID: t.GuildID, ChannelID: t.ChannelID, ClosedBy: in.Member.User.ID,
		ArchivedChannelID: reply.ArchivedChannelID,
	}
	if err := h.store.CloseTicket(ctx, done); err != nil {
		h.log.Error("ticket close not recorded", zap.String("channel_id", t.ChannelID), zap.Error(err))
		h.markPending(ctx, done)
	}
	if reply.TranscriptBody == "" {
		return
	}
	err := h.store.PutTranscript(ctx, discordstore.Transcript{
		TicketID: t.ID, Body: reply.TranscriptBody, MessageCount: reply.MessageCount,
	})
	if err != nil {
		h.log.Error("ticket transcript not stored", zap.Int("ticket_id", t.ID), zap.Error(err))
	}
}

func (h ticketModule) markPending(ctx context.Context, done discordstore.TicketClose) {
	if err := h.store.MarkPendingClose(ctx, done); err != nil {
		h.log.Error("pending close marker not written",
			zap.String("channel_id", done.ChannelID), zap.Error(err))
	}
}

func canCloseTicket(t discordstore.Ticket, in decode.InteractionEvent, cfg ddiscord.Config) bool {
	if t.OpenerID == in.Member.User.ID {
		return true
	}
	return isTicketStaffOrMod(cfg, in)
}

func (h ticketModule) add(ctx context.Context, call deskCall, sub decode.InteractionOption) error {
	t, ok := h.ticketFor(ctx, call)
	if !ok {
		return nil
	}
	if !canCloseTicket(t, call.in, call.mod.Config) {
		call.reply("Only the opener or ticket staff can add someone.")
		return nil
	}
	userID := decode.OptionUser(sub.Options, "user")
	if userID == "" {
		call.reply("Name the member to add.")
		return nil
	}
	reply, err := h.tickets.TicketAddMember(ctx, discordoutgress.TicketMemberAddRequest{ChannelID: t.ChannelID, UserID: userID})
	if rpcFailed(err, reply.Error) {
		h.log.Warn("ticket add failed", zap.Error(err), zap.String("outgress_error", reply.Error))
		call.reply("Could not add them right now.")
		return nil
	}
	call.reply("Added <@" + userID + "> to this ticket.")
	return nil
}
