// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"time"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

const (
	ticketOpenTimeout  = discordoutgress.TicketOpenServerTimeout
	ticketCloseTimeout = discordoutgress.TicketCloseServerTimeout
)

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

type summaryMemo interface {
	ClaimSummary(ctx context.Context, ticketID int) bool
}

type TicketDeps struct {
	Memo  summaryMemo
	BotID string
}

func SubscribeTickets(rest ticketREST, deps TicketDeps, wire Wiring) error {
	h := &ticketRPC{rest: rest, memo: deps.Memo, botID: deps.BotID, log: wire.Log}
	open := func(name string) verb { return verb{Name: name, Timeout: ticketOpenTimeout} }
	return errors.Join(
		register[discordoutgress.TicketOpenRequest, discordoutgress.TicketOpenReply](
			wire, open("ticket.open"), h.open),
		register[discordoutgress.TicketClaimRequest, discordoutgress.TicketClaimReply](
			wire, open("ticket.claim"), h.claim),
		register[discordoutgress.TicketMemberAddRequest, discordoutgress.TicketMemberAddReply](
			wire, open("ticket.add"), h.add),
		register[discordoutgress.TicketPanelRequest, discordoutgress.TicketPanelReply](
			wire, open("ticket.panel"), h.panel),
		register[discordoutgress.TicketCloseRequest, discordoutgress.TicketCloseReply](
			wire, verb{Name: "ticket.close", Timeout: ticketCloseTimeout}, h.close),
	)
}

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
		return discordoutgress.TicketOpenReply{ChannelID: got.ID, Error: err.Error(), Code: codeFor(err)}
	}
	return discordoutgress.TicketOpenReply{ChannelID: got.ID, MessageID: msg.ID}
}

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

func (h *ticketRPC) note(ctx context.Context, channelID, note string) {
	if note == "" {
		return
	}
	if err := h.rest.SendChat(ctx, discapi.ChatPost{ChannelID: channelID, Content: note}); err != nil {
		h.logger().Warn("ticket note failed", zap.String("channel_id", channelID), zap.Error(err))
	}
}

// Page the transcript before disposing of the channel: a deleted channel has no history.
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

func pageLimit(have int) int {
	left := ddiscord.TranscriptMessageCap - have
	if left > discapi.MessagePageMax {
		return discapi.MessagePageMax
	}
	return left
}

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

type summaryPost struct {
	req   discordoutgress.TicketCloseRequest
	body  string
	count int
}

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
	h.logger().Warn("ticket transcript upload failed",
		zap.String("channel_id", p.req.LogChannelID), zap.Error(err))
	h.sendSummary(ctx, p.req.LogChannelID, withUploadNote(embed))
}

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

const uploadFailedNote = "transcript upload failed"

func withUploadNote(embed ddiscord.Embed) ddiscord.Embed {
	if embed.Description == "" {
		embed.Description = uploadFailedNote
		return embed
	}
	embed.Description += "\n" + uploadFailedNote
	return embed
}

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

func archivedName(channelName string) string {
	if channelName == "" {
		return ""
	}
	return "closed-" + channelName
}

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

const (
	overwriteRole   = 0
	overwriteMember = 1
)

const (
	permNoBits           = "0"
	permViewBits         = "1024"
	permViewReadBits     = "66560"
	permTicketBotBits    = "109568"
	permTicketMemberBits = "68608"
)

func denyView(id string, kind int) discapi.PermissionOverwrite {
	return discapi.PermissionOverwrite{ID: id, Type: kind, Allow: permNoBits, Deny: permViewBits}
}

func allowRead(roleID string) discapi.PermissionOverwrite {
	return discapi.PermissionOverwrite{ID: roleID, Type: overwriteRole, Allow: permViewReadBits, Deny: permNoBits}
}
