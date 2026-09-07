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
	TicketPanel(ctx context.Context, req discordoutgress.TicketPanelRequest) (discordoutgress.TicketPanelReply, error)
	// ModifyChannel renames the channel once the ticket row exists; see
	// nameTicket for why the name cannot be final at create time.
	ModifyChannel(ctx context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error)
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

// deskCall is one interaction a ticket verb is answering: the module context
// it arrived on, the decoded interaction, and the emitter that carries the
// answer back. The three are never useful apart -- every verb needs the guild
// config from the context, the interaction's token to reply on, and the
// emitter to reply with -- so they travel as one value rather than as three
// parameters threaded through every path. Passing them separately is what put
// five arguments on postPanel, recordTicket, recordClaim, finishClose and add
// at once; grouping them fixes the shape rather than one signature.
type deskCall struct {
	mod  *module.Context
	in   decode.InteractionEvent
	emit module.Emit
}

// deskCallFrom decodes the interaction a slash command or button arrived on.
// The decode failing is the one case a ticket verb reports as an error rather
// than as a reply: there is no token to reply on.
func deskCallFrom(c *module.Context, emit module.Emit) (deskCall, error) {
	in, err := decode.Decode[decode.InteractionEvent](c.Event.Raw)
	if err != nil {
		return deskCall{}, err
	}
	return deskCall{mod: c, in: in, emit: emit}, nil
}

// reply is the one shape every desk outcome takes: an ephemeral followup on
// the interaction that triggered it. Every path answers, including the
// refusals -- a button that appears to do nothing is the worst outcome here.
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
	// Posting the desk panel is a staff action, not a member one. It was
	// ungated, which meant any member could paste a second "Open a ticket"
	// panel into any channel they could run a slash command in -- and because
	// the panel's button is the real one, the tickets it opened were real too.
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

// postPanel posts the panel through the RPC that returns its message id, and
// remembers the pointer only once there IS one.
//
// The old shape emitted a fire-and-forget PostPanel Command and then wrote a
// desk pointer with an empty message id. That pointer is worse than none: the
// repost path reads it, finds nothing to delete, and stacks a second live
// panel under the first -- and the write had already erased the id of the
// panel that was actually posted.
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
		// The panel IS posted; only the pointer is missing. Say the panel is
		// up rather than implying it is not.
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
		// Refusing beats opening. Without discord-data there is no row id to
		// name the channel after, no open limit, and no transcript -- so an
		// "open" here hands a member a private channel the desk can never
		// number, cap or close cleanly. ERROR per attempt, not once at boot:
		// the operator needs the volume to see it is not a one-off.
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

// atLimitText names the number back to the opener rather than saying "too
// many": a streamer who set the cap to 3 gets a message that matches what they
// configured.
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
		h.rollback(ctx, reply.ChannelID)
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
		// The channel exists but no row points at it: delete it rather than
		// leave a ticket the desk can never close, claim or transcribe.
		h.log.Error("ticket row not recorded; rolling the channel back",
			zap.String("channel_id", reply.ChannelID), zap.Error(err))
		h.rollback(ctx, reply.ChannelID)
		call.reply("Could not open a ticket right now.")
		return nil
	}
	if got.AtLimit {
		// The pre-check passed and the insert still refused: two presses
		// raced. Same rollback, and the opener is told the real number.
		h.rollback(ctx, reply.ChannelID)
		call.reply(atLimitText(got.OpenCount))
		return nil
	}
	h.nameTicket(ctx, in, reply.ChannelID, got.TicketID)
	call.reply("Ticket opened: <#" + reply.ChannelID + ">")
	return nil
}

// nameTicket renames the fresh channel to carry the ticket ROW's id.
//
// It is a second call because the id does not exist until the row does, and
// the row cannot exist until the channel does (channel_id is the row's unique
// key). The number used to be the opener's nth LIVE ticket, which collides the
// moment a member closes one and opens another: two "ticket-ada-1" channels,
// and after archiving two "closed-ticket-ada-1" ones. Best effort -- a channel
// that keeps the unnumbered name is cosmetic, and every other path keys on the
// channel id.
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

// ticketNameBase is what the channel name is built from: the opener's
// username, or their id when the username has nothing Discord's channel-name
// charset keeps (see ddiscord.SanitizeChannelName).
func ticketNameBase(in decode.InteractionEvent) string {
	if name := ddiscord.SanitizeChannelName(in.Member.User.Username); name != "" {
		return name
	}
	return in.Member.User.ID
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

// ticketFor loads the ticket this interaction is sitting in, and is the ONLY
// way the claim/close/add paths get one.
//
// The lookup is scoped to the interaction's guild. That scope is not
// theoretical bookkeeping: the ticket keyspace is keyed on the channel id
// alone, so an unscoped row written for one guild answers a lookup made from
// any other, and the desk would then let a member of guild B claim, close or
// add people to guild A's private support channel. Merge note (2026-09-05):
// this used to read the ticket unscoped and compare t.GuildID here; the guild
// moved into the Store call so discord-data applies the same filter in SQL,
// which is where a caller that forgets the check cannot get past it.
//
// It also finishes any close that Discord performed but the row never
// recorded (see markPending): retrying here, on the next interaction that
// touches the channel, is the whole recovery mechanism -- no sweeper, no
// timer, and nothing to leak when the marker's day expires unused.
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

// retryPendingClose finishes a close whose store write failed the first time.
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
		// The claim is recorded; only the card is stale. Say so rather than
		// implying the claim failed.
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
		// The row survives a close (discord-data keeps it, and an archived
		// channel keeps its buttons), so the close button is still pressable
		// on a ticket that is already done. Running the sequence again would
		// re-page a channel that may no longer exist and post a second summary.
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

// storeClose records the terminal state and the transcript. Both writes are
// best effort at this point: Discord has already archived or deleted the
// channel, and failing the interaction now would tell the user the close did
// not happen when it did.
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

// markPending records a close Discord already performed that the row never
// took, so the next interaction on the channel can finish the write. Without
// it the row says "open" forever: the channel is gone or archived, so nothing
// can press the button that would try again, and the opener's open-ticket
// count never comes back down -- they hit the limit and can never open
// another.
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

// add grants one member access to this ticket (/ticket add user:<@user>).
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
