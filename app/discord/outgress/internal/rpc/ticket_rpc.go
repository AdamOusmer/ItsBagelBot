// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

// ticketOpenTimeout bounds a create-channel plus a post: two REST calls.
const ticketOpenTimeout = 15 * time.Second

// ticketCloseTimeout bounds the whole close sequence. A transcript pages the
// channel up to TranscriptMessageCap/MessagePageMax = 20 times, then uploads a
// file and posts a summary, then moves or deletes the channel: 23 REST calls
// worst case. At Discord's shared ~50 req/s budget that is well under a
// second of call time, but each one can sit behind a Retry-After from the same
// bucket every other guild is drawing on, so the ceiling is set by waiting,
// not by working. 60s clears that; the engine's own 8s client timeout means a
// close that genuinely takes this long has already told the user it is working
// and finishes in the background.
const ticketCloseTimeout = 60 * time.Second

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

// SubscribeTickets wires the ticket-desk orchestrations (see
// internal/domain/rpc/discordoutgress/ticket.go for why they are RPCs). Split
// from SubscribeEngine so neither function is a wall of registrations.
func SubscribeTickets(rest ticketREST, wire EngineWiring) error {
	h := &ticketRPC{rest: rest, log: wire.Log}
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
	return bus.QueueSubscribeJSON[discordoutgress.TicketCloseRequest, discordoutgress.TicketCloseReply](
		wire.NC, wire.Prefix+".ticket.close", wire.Queue, ticketCloseTimeout, wire.App, wire.Log, h.close)
}

// add grants one member VIEW|SEND|READ_HISTORY on the ticket channel.
func (h *ticketRPC) add(ctx context.Context, req discordoutgress.TicketMemberAddRequest) discordoutgress.TicketMemberAddReply {
	err := h.rest.SetChannelOverwrite(ctx, discapi.ChannelOverwrite{
		ChannelID: req.ChannelID,
		Overwrite: discapi.PermissionOverwrite{ID: req.UserID, Type: overwriteMember, Allow: permTicketMemberBits},
	})
	if err != nil {
		return discordoutgress.TicketMemberAddReply{Error: err.Error(), Code: codeFor(err)}
	}
	return discordoutgress.TicketMemberAddReply{}
}

type ticketRPC struct {
	rest ticketREST
	log  *zap.Logger
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
			PermissionOverwrites: req.Overwrites,
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

func (h *ticketRPC) close(ctx context.Context, req discordoutgress.TicketCloseRequest) discordoutgress.TicketCloseReply {
	body, count := h.transcript(ctx, req)
	h.postSummary(ctx, req, body, count)
	archived, err := h.disposeChannel(ctx, req)
	if err != nil {
		return discordoutgress.TicketCloseReply{
			MessageCount: count, TranscriptBody: body,
			Error: err.Error(), Code: codeFor(err),
		}
	}
	return discordoutgress.TicketCloseReply{MessageCount: count, TranscriptBody: body, ArchivedChannelID: archived}
}

// transcript pages the channel and renders it, or returns nothing when the
// guild turned transcripts off. A paging failure is logged and degrades to the
// partial transcript collected so far: losing the close summary because page
// 14 of 20 hit a 429 would be the worse outcome.
func (h *ticketRPC) transcript(ctx context.Context, req discordoutgress.TicketCloseRequest) (string, int) {
	if !req.Transcript {
		return "", 0
	}
	msgs, err := h.collect(ctx, req.ChannelID)
	if err != nil {
		h.logger().Warn("ticket transcript paging failed",
			zap.String("channel_id", req.ChannelID), zap.Int("collected", len(msgs)), zap.Error(err))
	}
	if len(msgs) == 0 {
		return "", 0
	}
	doc := ddiscord.TranscriptDoc{ChannelName: req.ChannelName, Messages: transcriptMessages(msgs)}
	return ddiscord.RenderTranscript(doc), len(msgs)
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
			Attachments: attachmentURLs(m.Attachments),
		})
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

// postSummary posts the close card, with the transcript attached when there is
// one. Best effort in both halves: the channel is about to be archived or
// deleted either way, and a guild with no log channel configured simply gets
// no summary.
func (h *ticketRPC) postSummary(ctx context.Context, req discordoutgress.TicketCloseRequest, body string, count int) {
	if req.LogChannelID == "" {
		return
	}
	embed := ddiscord.TicketClosedEmbed(ddiscord.TicketClosed{
		Opener: req.Summary.Opener, Closer: req.Summary.Closer,
		Duration: openFor(req.Summary.OpenedAtUnixMs), MessageCount: count,
		ChannelName: req.ChannelName,
	})
	if body == "" {
		if _, err := h.rest.SendEmbed(ctx, discapi.EmbedPost{ChannelID: req.LogChannelID, Embed: embed}); err != nil {
			h.logger().Warn("ticket close summary failed", zap.String("channel_id", req.LogChannelID), zap.Error(err))
		}
		return
	}
	_, err := h.rest.SendFile(ctx, discapi.FileUpload{
		ChannelID: req.LogChannelID, Filename: transcriptFilename(req.ChannelName),
		Data: []byte(body), Embed: &embed,
	})
	if err != nil {
		h.logger().Warn("ticket transcript upload failed", zap.String("channel_id", req.LogChannelID), zap.Error(err))
	}
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
	permViewBits     = "1024"
	permViewReadBits = "66560"
	// permTicketMemberBits is VIEW|SEND|READ_HISTORY: what a member added to
	// someone else's ticket needs to read it and answer in it, and nothing
	// more.
	permTicketMemberBits = "68608"
)

func denyView(id string, kind int) discapi.PermissionOverwrite {
	return discapi.PermissionOverwrite{ID: id, Type: kind, Deny: permViewBits}
}

func allowRead(roleID string) discapi.PermissionOverwrite {
	return discapi.PermissionOverwrite{ID: roleID, Type: overwriteRole, Allow: permViewReadBits}
}
