// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// The desk's handler deadlines come from the shared table in
// internal/domain/rpc/discordoutgress/timeouts.go, which pairs each with the
// client deadline engine waits under. They are NOT written here: the two used
// to live in two files and drifted, with the engine giving up before outgress
// stopped -- see that file's doc for the outage shape that produces.
const (
	ticketOpenTimeout  = discordoutgress.TicketOpenServerTimeout
	ticketCloseTimeout = discordoutgress.TicketCloseServerTimeout
)

// ticketREST is the REST slice the desk orchestrations need.
type ticketREST interface {
	CreateChannel(ctx context.Context, ch discapi.GuildChannel) (discapi.Snowflake, error)
	DeleteChannel(ctx context.Context, ch discapi.Snowflake) error
	ModifyChannel(ctx context.Context, patch discapi.ChannelPatch) error
	SendChat(ctx context.Context, post discapi.ChatPost) error
	SendEmbed(ctx context.Context, post discapi.EmbedPost) (discapi.Message, error)
	SendPanel(ctx context.Context, post discapi.EmbedPost, buttons []discapi.Button) (discapi.Message, error)
	EditMessage(ctx context.Context, m discapi.Message, patch discapi.MessagePatch) error
	ListMessagesFull(ctx context.Context, page discapi.MessagePage) ([]discapi.FullMessage, error)
	SendFile(ctx context.Context, up discapi.FileUpload) (discapi.Message, error)
	SetChannelOverwrite(ctx context.Context, o discapi.ChannelOverwrite) error
}

// summaryMemo is the one thing the close path needs from the shared store: a
// set-if-absent per ticket id, so a retried close does not post the summary
// and upload the transcript twice. Declared as a one-method interface rather
// than taking discordstore.Store whole, because that is the whole of the
// dependency and a test can supply it in three lines.
type summaryMemo interface {
	ClaimSummary(ctx context.Context, ticketID int) bool
}

// TicketDeps is what the desk handlers need beyond REST.
type TicketDeps struct {
	// Memo makes the close summary idempotent. Nil posts every time.
	Memo summaryMemo
	// BotID is the application id, which for a bot account IS the bot user's
	// snowflake. It is used to grant the bot itself an explicit overwrite on
	// every ticket channel: a ticket category whose @everyone deny the bot
	// inherits leaves the desk unable to read its own ticket, so the close
	// pages an empty history and the transcript comes out blank.
	BotID string
}

// SubscribeTickets wires the ticket-desk orchestrations (see
// internal/domain/rpc/discordoutgress/ticket.go for why they are RPCs). Split
// from SubscribeEngine so neither function is a wall of registrations.
func SubscribeTickets(rest ticketREST, deps TicketDeps, wire EngineWiring) error {
	h := &ticketRPC{rest: rest, memo: deps.Memo, botID: deps.BotID, log: wire.Log}
	if err := bus.QueueSubscribeJSON[discordoutgress.TicketOpenRequest, discordoutgress.TicketOpenReply](
		wire.NC, wire.Prefix+".ticket.open", wire.Queue, ticketOpenTimeout, wire.App, wire.Log, h.open); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.TicketClaimRequest, discordoutgress.TicketClaimReply](
		wire.NC, wire.Prefix+".ticket.claim", wire.Queue, ticketOpenTimeout, wire.App, wire.Log, h.claim); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.TicketMemberAddRequest, discordoutgress.TicketMemberAddReply](
		wire.NC, wire.Prefix+".ticket.add", wire.Queue, ticketOpenTimeout, wire.App, wire.Log, h.add); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discordoutgress.TicketPanelRequest, discordoutgress.TicketPanelReply](
		wire.NC, wire.Prefix+".ticket.panel", wire.Queue, ticketOpenTimeout, wire.App, wire.Log, h.panel); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[discordoutgress.TicketCloseRequest, discordoutgress.TicketCloseReply](
		wire.NC, wire.Prefix+".ticket.close", wire.Queue, ticketCloseTimeout, wire.App, wire.Log, h.close)
}

// add grants one member VIEW|SEND|READ_HISTORY on the ticket channel.
func (h *ticketRPC) add(ctx context.Context, req discordoutgress.TicketMemberAddRequest) discordoutgress.TicketMemberAddReply {
	err := h.rest.SetChannelOverwrite(ctx, discapi.ChannelOverwrite{
		ChannelID: req.ChannelID,
		Overwrite: discapi.PermissionOverwrite{
			ID: req.UserID, Type: overwriteMember, Allow: permTicketMemberBits, Deny: permNoBits,
		},
	})
	if err != nil {
		return discordoutgress.TicketMemberAddReply{Error: err.Error(), Code: codeFor(err)}
	}
	return discordoutgress.TicketMemberAddReply{}
}

type ticketRPC struct {
	rest  ticketREST
	memo  summaryMemo
	botID string
	log   *zap.Logger
}

// panel posts the persistent desk panel and returns its message id, which the
// engine stores so a later repost can delete this one instead of stacking a
// second panel under it.
func (h *ticketRPC) panel(ctx context.Context, req discordoutgress.TicketPanelRequest) discordoutgress.TicketPanelReply {
	if req.ChannelID == "" {
		return discordoutgress.TicketPanelReply{Error: "missing channel_id", Code: outgressrpc.CodeInvalid}
	}
	msg, err := h.rest.SendPanel(ctx,
		discapi.EmbedPost{ChannelID: req.ChannelID, Content: req.Content, Embed: req.Embed},
		ticketButtons(req.Buttons))
	if err != nil {
		return discordoutgress.TicketPanelReply{Error: err.Error(), Code: codeFor(err)}
	}
	return discordoutgress.TicketPanelReply{MessageID: msg.ID}
}

func (h *ticketRPC) logger() *zap.Logger {
	if h.log == nil {
		return zap.NewNop()
	}
	return h.log
}

func (h *ticketRPC) open(ctx context.Context, req discordoutgress.TicketOpenRequest) discordoutgress.TicketOpenReply {
	got, err := h.rest.CreateChannel(ctx, discapi.GuildChannel{
		Guild: discapi.Guild{ID: req.GuildID},
		Spec: discapi.ChannelCreate{
			Name: req.Name, Type: ddiscord.ChannelText, ParentID: req.ParentID,
			PermissionOverwrites: h.withBotOverwrite(req.Overwrites),
		},
	})
	if err != nil {
		return discordoutgress.TicketOpenReply{Error: err.Error(), Code: codeFor(err)}
	}
	msg, err := h.rest.SendPanel(ctx,
		discapi.EmbedPost{ChannelID: got.ID, Content: req.Content, Embed: req.Embed}, ticketButtons(req.Buttons))
	if err != nil {
		// The channel exists either way, and the reply says so: the caller
		// must record or roll it back rather than leak an orphan channel that
		// no ticket row points at.
		return discordoutgress.TicketOpenReply{ChannelID: got.ID, Error: err.Error(), Code: codeFor(err)}
	}
	return discordoutgress.TicketOpenReply{ChannelID: got.ID, MessageID: msg.ID}
}

// withBotOverwrite appends the bot's own allow overwrite to the engine's set.
//
// It is added HERE, not in the engine, because only this process knows the
// application id -- the engine never sees a bot token. Without it the ticket
// channel's permissions are whatever the bot inherits from the category, and a
// staff-only ticket category that denies @everyone denies the bot too: the
// card posts (the create still carries MANAGE_CHANNELS), and then every later
// call into the channel 403s. The visible failure is a blank transcript on
// close, which reads as a transcript bug rather than a permissions one.
func (h *ticketRPC) withBotOverwrite(in []discapi.PermissionOverwrite) []discapi.PermissionOverwrite {
	if h.botID == "" {
		return in
	}
	return append(append([]discapi.PermissionOverwrite{}, in...), discapi.PermissionOverwrite{
		ID: h.botID, Type: overwriteMember, Allow: permTicketBotBits, Deny: permNoBits,
	})
}

func ticketButtons(specs []ddiscord.ButtonSpec) []discapi.Button {
	out := make([]discapi.Button, 0, len(specs))
	for _, spec := range specs {
		out = append(out, discapi.Button{Style: spec.Style, Label: spec.Label, CustomID: spec.CustomID})
	}
	return out
}

func (h *ticketRPC) claim(ctx context.Context, req discordoutgress.TicketClaimRequest) discordoutgress.TicketClaimReply {
	if req.MessageID == "" {
		// A ticket opened before the card's id was recorded has nothing to
		// edit. The claim itself already succeeded in the database, so this is
		// not an error the user should see.
		return discordoutgress.TicketClaimReply{}
	}
	err := h.rest.EditMessage(ctx,
		discapi.Message{ChannelID: req.ChannelID, ID: req.MessageID},
		discapi.MessagePatch{Content: req.Content, Embeds: []ddiscord.Embed{req.Embed}})
	if err != nil {
		return discordoutgress.TicketClaimReply{Error: err.Error(), Code: codeFor(err)}
	}
	h.note(ctx, req.ChannelID, req.Note)
	return discordoutgress.TicketClaimReply{}
}

// note posts the in-channel line. Best effort: the card already carries the
// claim, so a failed note is not worth failing the claim over.
func (h *ticketRPC) note(ctx context.Context, channelID, note string) {
	if note == "" {
		return
	}
	if err := h.rest.SendChat(ctx, discapi.ChatPost{ChannelID: channelID, Content: note}); err != nil {
		h.logger().Warn("ticket note failed", zap.String("channel_id", channelID), zap.Error(err))
	}
}

// close pages the history, DISPOSES of the channel, and only then posts the
// summary.
//
// The dispose comes before the post on purpose. It used to come after, and the
// ordering matters because the summary is the step most likely to fail slowly:
// the log channel can be missing, forbidden, or rate-limited, and a close that
// dies in the log channel used to leave the ticket channel still sitting in
// the open category -- visible, writable, with a closed row behind it. Posting
// last means the worst outcome is a closed ticket with no card in the log,
// which is a missing record rather than a broken desk. The transcript is still
// collected FIRST, because a deleted channel has no history left to page.
func (h *ticketRPC) close(ctx context.Context, req discordoutgress.TicketCloseRequest) discordoutgress.TicketCloseReply {
	body, count, truncated := h.transcript(ctx, req)
	archived, err := h.disposeChannel(ctx, req)
	h.postSummary(ctx, summaryPost{req: req, body: body, count: count})
	reply := discordoutgress.TicketCloseReply{
		MessageCount: count, TranscriptBody: body, Truncated: truncated, ArchivedChannelID: archived,
	}
	if err != nil {
		reply.ArchivedChannelID = ""
		reply.Error = err.Error()
		reply.Code = codeFor(err)
	}
	return reply
}

// transcript pages the channel and renders it, or returns nothing when the
// guild turned transcripts off. A paging failure is logged and degrades to the
// partial transcript collected so far: losing the close summary because page
// 14 of 20 hit a 429 would be the worse outcome.
func (h *ticketRPC) transcript(ctx context.Context, req discordoutgress.TicketCloseRequest) (string, int, bool) {
	if !req.Transcript {
		return "", 0, false
	}
	msgs, err := h.collect(ctx, req.ChannelID)
	if err != nil {
		h.logger().Warn("ticket transcript paging failed",
			zap.String("channel_id", req.ChannelID), zap.Int("collected", len(msgs)), zap.Error(err))
	}
	if len(msgs) == 0 {
		return "", 0, err != nil
	}
	truncated := err != nil || len(msgs) >= ddiscord.TranscriptMessageCap
	doc := ddiscord.TranscriptDoc{
		ChannelName: req.ChannelName, Messages: transcriptMessages(msgs), Truncated: truncated,
	}
	return ddiscord.RenderTranscript(doc), len(msgs), truncated
}

// collect pages the channel newest-first with a before-cursor until Discord
// returns a short page or the cap is reached. The returned slice is in
// Discord's own order (newest first); transcriptMessages reverses it.
func (h *ticketRPC) collect(ctx context.Context, channelID string) ([]discapi.FullMessage, error) {
	var out []discapi.FullMessage
	before := ""
	for len(out) < ddiscord.TranscriptMessageCap {
		page, err := h.rest.ListMessagesFull(ctx,
			discapi.MessagePage{ChannelID: channelID, Before: before, Limit: pageLimit(len(out))})
		if err != nil {
			return out, err
		}
		if len(page) == 0 {
			return out, nil
		}
		out = append(out, page...)
		before = page[len(page)-1].ID
		if len(page) < discapi.MessagePageMax {
			return out, nil
		}
	}
	return out, nil
}

// pageLimit asks for only as many as the cap still allows, so the last page
// does not overshoot TranscriptMessageCap and get trimmed after the fact.
func pageLimit(have int) int {
	left := ddiscord.TranscriptMessageCap - have
	if left > discapi.MessagePageMax {
		return discapi.MessagePageMax
	}
	return left
}

// transcriptMessages maps the REST page onto the domain's render input,
// reversing into conversation order (oldest first).
func transcriptMessages(in []discapi.FullMessage) []ddiscord.TranscriptMessage {
	out := make([]ddiscord.TranscriptMessage, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		m := in[i]
		out = append(out, ddiscord.TranscriptMessage{
			AuthorName: m.Author.DisplayName(), Content: m.Content, At: m.At(),
			Attachments: attachmentURLs(m.Attachments), Embeds: transcriptEmbeds(m.Embeds),
		})
	}
	return out
}

func transcriptEmbeds(in []discapi.MessageEmbed) []ddiscord.TranscriptEmbed {
	if len(in) == 0 {
		return nil
	}
	out := make([]ddiscord.TranscriptEmbed, 0, len(in))
	for _, e := range in {
		out = append(out, ddiscord.TranscriptEmbed{Title: e.Title, Description: e.Description})
	}
	return out
}

func attachmentURLs(in []discapi.MessageAttachment) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, a.URL)
	}
	return out
}

// summaryPost is the close card's inputs as one value: postSummary and its
// two halves would otherwise pass four positional arguments between them.
type summaryPost struct {
	req   discordoutgress.TicketCloseRequest
	body  string
	count int
}

// postSummary posts the close card, with the transcript attached when there is
// one, exactly once per ticket. Best effort throughout: the channel is already
// archived or deleted by the time this runs, and a guild with no log channel
// configured simply gets no summary.
func (h *ticketRPC) postSummary(ctx context.Context, p summaryPost) {
	if p.req.LogChannelID == "" || !h.claimSummary(ctx, p.req.TicketID) {
		return
	}
	embed := ddiscord.TicketClosedEmbed(ddiscord.TicketClosed{
		Opener: p.req.Summary.Opener, Closer: p.req.Summary.Closer,
		Duration: openFor(p.req.Summary.OpenedAtUnixMs), MessageCount: p.count,
		ChannelName: p.req.ChannelName,
	})
	if p.body == "" {
		h.sendSummary(ctx, p.req.LogChannelID, embed)
		return
	}
	err := h.sendTranscript(ctx, p, embed)
	if err == nil {
		return
	}
	// The upload is the half that fails on its own: a 40005 (payload too
	// large) or a proxy that rejects multipart takes the CARD with it if the
	// card only ever travelled attached to the file. Posting the card again on
	// its own, saying so, is what keeps the close visible in the log.
	h.logger().Warn("ticket transcript upload failed",
		zap.String("channel_id", p.req.LogChannelID), zap.Error(err))
	h.sendSummary(ctx, p.req.LogChannelID, withUploadNote(embed))
}

// claimSummary reports whether this close is the one that posts. Without a
// memo (or without a ticket id, which the pure-Valkey fallback never has)
// every close posts, which is the old behaviour.
func (h *ticketRPC) claimSummary(ctx context.Context, ticketID int) bool {
	if h.memo == nil {
		return true
	}
	return h.memo.ClaimSummary(ctx, ticketID)
}

func (h *ticketRPC) sendTranscript(ctx context.Context, p summaryPost, embed ddiscord.Embed) error {
	_, err := h.rest.SendFile(ctx, discapi.FileUpload{
		ChannelID: p.req.LogChannelID, Filename: transcriptFilename(p.req.ChannelName),
		Data: []byte(p.body), Embed: &embed,
	})
	return err
}

func (h *ticketRPC) sendSummary(ctx context.Context, channelID string, embed ddiscord.Embed) {
	if _, err := h.rest.SendEmbed(ctx, discapi.EmbedPost{ChannelID: channelID, Embed: embed}); err != nil {
		h.logger().Warn("ticket close summary failed", zap.String("channel_id", channelID), zap.Error(err))
	}
}

// uploadFailedNote is what the fallback card says instead of the file.
const uploadFailedNote = "transcript upload failed"

func withUploadNote(embed ddiscord.Embed) ddiscord.Embed {
	if embed.Description == "" {
		embed.Description = uploadFailedNote
		return embed
	}
	embed.Description += "\n" + uploadFailedNote
	return embed
}

// openFor is how long the ticket was open. A zero or future opened_at yields
// zero rather than a negative or absurd duration: the card would rather say
// "0m" than lie about a clock skew.
func openFor(openedAtUnixMs int64) time.Duration {
	if openedAtUnixMs <= 0 {
		return 0
	}
	d := time.Since(time.UnixMilli(openedAtUnixMs))
	if d < 0 {
		return 0
	}
	return d
}

func transcriptFilename(channelName string) string {
	if channelName == "" {
		return "transcript.txt"
	}
	return channelName + ".txt"
}

// disposeChannel archives the channel when the guild configured a category for
// it, and deletes it otherwise. Archiving returns the (unchanged) channel id so
// the ticket row can point at a channel that still exists.
func (h *ticketRPC) disposeChannel(ctx context.Context, req discordoutgress.TicketCloseRequest) (string, error) {
	if req.ArchiveCategoryID == "" {
		return "", h.rest.DeleteChannel(ctx, discapi.Snowflake{ID: req.ChannelID})
	}
	parent := req.ArchiveCategoryID
	err := h.rest.ModifyChannel(ctx, discapi.ChannelPatch{
		ID:                   req.ChannelID,
		Name:                 archivedName(req.ChannelName),
		ParentID:             &parent,
		PermissionOverwrites: archiveOverwrites(req),
	})
	if err != nil {
		return "", err
	}
	return req.ChannelID, nil
}

// archivedName prefixes the closed channel so the archive category reads as a
// list of closed tickets rather than a second set of live ones.
func archivedName(channelName string) string {
	if channelName == "" {
		return ""
	}
	return "closed-" + channelName
}

// archiveOverwrites is the archived channel's permission set: @everyone and
// the opener denied VIEW, staff keeping it. The opener loses access on purpose
// -- the archive is the staff's record, and the transcript is what the opener
// keeps.
func archiveOverwrites(req discordoutgress.TicketCloseRequest) []discapi.PermissionOverwrite {
	out := []discapi.PermissionOverwrite{denyView(req.GuildID, overwriteRole)}
	if req.OpenerID != "" {
		out = append(out, denyView(req.OpenerID, overwriteMember))
	}
	for _, roleID := range req.StaffRoleIDs {
		if roleID == "" {
			continue
		}
		out = append(out, allowRead(roleID))
	}
	return out
}

// Discord's overwrite target kinds.
const (
	overwriteRole   = 0
	overwriteMember = 1
)

// Permission bits used by the archive overwrites, as Discord's own decimal
// strings. VIEW_CHANNEL is 1<<10, READ_MESSAGE_HISTORY is 1<<16.
const (
	// permNoBits is the "grants nothing / denies nothing" half of an
	// overwrite. Discord's overwrite object types allow and deny as
	// STRINGS, and an omitted or empty one is not the same as "0" on the
	// receiving end -- an empty string is rejected outright by newer API
	// versions, and older ones treated it as "leave the existing value",
	// which on an archive PATCH means the deny we are writing lands on top
	// of an allow we meant to clear. decode.OverwriteAllow/OverwriteDeny on
	// the engine side have always written "0" for the unused half; these are
	// the same convention on this side.
	permNoBits       = "0"
	permViewBits     = "1024"
	permViewReadBits = "66560"
	// permTicketBotBits is VIEW|SEND|READ_HISTORY|MANAGE_MESSAGES|
	// ATTACH_FILES (1<<10 | 1<<11 | 1<<16 | 1<<13 | 1<<15): what the desk
	// itself needs inside a ticket -- read the history it transcribes, post
	// and edit the card, pin and clean up, and attach the transcript.
	permTicketBotBits = "109568"
	// permTicketMemberBits is VIEW|SEND|READ_HISTORY: what a member added to
	// someone else's ticket needs to read it and answer in it, and nothing
	// more.
	permTicketMemberBits = "68608"
)

func denyView(id string, kind int) discapi.PermissionOverwrite {
	return discapi.PermissionOverwrite{ID: id, Type: kind, Allow: permNoBits, Deny: permViewBits}
}

func allowRead(roleID string) discapi.PermissionOverwrite {
	return discapi.PermissionOverwrite{ID: roleID, Type: overwriteRole, Allow: permViewReadBits, Deny: permNoBits}
}
