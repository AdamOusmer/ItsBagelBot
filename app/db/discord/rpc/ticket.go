// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/pkg/bus"
)

type ticketRPC struct{ repo TicketStore }

// subscribeTickets registers the ticket-desk and transcript verbs.
func subscribeTickets(w Wiring) error {
	h := ticketRPC{repo: w.Repo}
	if err := bus.QueueSubscribeJSON[discorddata.TicketOpenRequest, discorddata.TicketOpenReply](
		w.NC, w.subject(discorddata.VerbTicketOpen), w.QueueGroup, requestTimeout, w.App, w.Log, h.open); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discorddata.TicketClaimRequest, discorddata.TicketClaimReply](
		w.NC, w.subject(discorddata.VerbTicketClaim), w.QueueGroup, requestTimeout, w.App, w.Log, h.claim); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discorddata.TicketCloseRequest, discorddata.TicketCloseReply](
		w.NC, w.subject(discorddata.VerbTicketClose), w.QueueGroup, requestTimeout, w.App, w.Log, h.close); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discorddata.TicketGetRequest, discorddata.TicketGetReply](
		w.NC, w.subject(discorddata.VerbTicketGet), w.QueueGroup, requestTimeout, w.App, w.Log, h.get); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discorddata.TicketCountRequest, discorddata.TicketCountReply](
		w.NC, w.subject(discorddata.VerbTicketCount), w.QueueGroup, requestTimeout, w.App, w.Log, h.count); err != nil {
		return err
	}
	if err := bus.QueueSubscribeJSON[discorddata.TicketListRequest, discorddata.TicketListReply](
		w.NC, w.subject(discorddata.VerbTicketList), w.QueueGroup, requestTimeout, w.App, w.Log, h.list); err != nil {
		return err
	}
	return subscribeTranscripts(w, h)
}

// subscribeTranscripts registers the two transcript verbs. Split from
// subscribeTickets so neither function is a wall of near-identical
// registrations.
func subscribeTranscripts(w Wiring, h ticketRPC) error {
	if err := bus.QueueSubscribeJSON[discorddata.TranscriptPutRequest, discorddata.TranscriptPutReply](
		w.NC, w.subject(discorddata.VerbTranscriptPut), w.QueueGroup, requestTimeout, w.App, w.Log, h.transcriptPut); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[discorddata.TranscriptGetRequest, discorddata.TranscriptGetReply](
		w.NC, w.subject(discorddata.VerbTranscriptGet), w.QueueGroup, requestTimeout, w.App, w.Log, h.transcriptGet)
}

func (h ticketRPC) open(ctx context.Context, req discorddata.TicketOpenRequest) discorddata.TicketOpenReply {
	id, count, err := h.repo.TicketOpen(ctx, repository.OpenParams{
		GuildID:   req.GuildID,
		ChannelID: req.ChannelID,
		OpenerID:  req.OpenerID,
		Subject:   req.Subject,
		Limit:     req.OpenLimit,

		PanelMessageID: req.PanelMessageID,
	})
	if err != nil {
		message, code := failure(err)
		// The count travels even on a refusal: at CodeLimit it is how many
		// tickets the opener already holds, which is what the ephemeral reply
		// names back to them.
		return discorddata.TicketOpenReply{OpenCount: count, Error: message, Code: code}
	}
	return discorddata.TicketOpenReply{TicketID: id, OpenCount: count}
}

func (h ticketRPC) claim(ctx context.Context, req discorddata.TicketClaimRequest) discorddata.TicketClaimReply {
	id, err := h.repo.TicketClaim(ctx, req.GuildID, req.ChannelID, req.StaffID)
	if err != nil {
		message, code := failure(err)
		return discorddata.TicketClaimReply{Error: message, Code: code}
	}
	return discorddata.TicketClaimReply{TicketID: id}
}

func (h ticketRPC) close(ctx context.Context, req discorddata.TicketCloseRequest) discorddata.TicketCloseReply {
	id, openerID, err := h.repo.TicketClose(ctx, repository.CloseParams{
		GuildID:           req.GuildID,
		ChannelID:         req.ChannelID,
		ClosedBy:          req.ClosedBy,
		ArchivedChannelID: req.ArchivedChannelID,
	})
	if err != nil {
		message, code := failure(err)
		return discorddata.TicketCloseReply{Error: message, Code: code}
	}
	return discorddata.TicketCloseReply{TicketID: id, OpenerID: openerID}
}

func (h ticketRPC) get(ctx context.Context, req discorddata.TicketGetRequest) discorddata.TicketGetReply {
	row, found, err := h.repo.TicketGet(ctx, req.GuildID, req.ChannelID)
	if err != nil {
		message, code := failure(err)
		return discorddata.TicketGetReply{Error: message, Code: code}
	}
	if !found {
		return discorddata.TicketGetReply{}
	}
	return discorddata.TicketGetReply{Ticket: ticketView(row), Found: true}
}

func (h ticketRPC) count(ctx context.Context, req discorddata.TicketCountRequest) discorddata.TicketCountReply {
	n, err := h.repo.TicketOpenCount(ctx, req.GuildID, req.OpenerID)
	if err != nil {
		message, code := failure(err)
		return discorddata.TicketCountReply{Error: message, Code: code}
	}
	return discorddata.TicketCountReply{Count: n}
}

func (h ticketRPC) list(ctx context.Context, req discorddata.TicketListRequest) discorddata.TicketListReply {
	rows, next, err := h.repo.TicketList(ctx, repository.ListParams{
		GuildID: req.GuildID,
		Status:  req.Status,
		Limit:   req.Limit,
		Cursor:  req.Cursor,
	})
	if err != nil {
		message, code := failure(err)
		return discorddata.TicketListReply{Error: message, Code: code}
	}
	views := make([]discorddata.Ticket, 0, len(rows))
	for _, row := range rows {
		views = append(views, ticketView(row))
	}
	return discorddata.TicketListReply{Tickets: views, NextCursor: next}
}

func (h ticketRPC) transcriptPut(ctx context.Context, req discorddata.TranscriptPutRequest) discorddata.TranscriptPutReply {
	err := h.repo.TranscriptPut(ctx, req.TicketID, req.Body, req.MessageCount)
	message, code := failure(err)
	return discorddata.TranscriptPutReply{Error: message, Code: code}
}

func (h ticketRPC) transcriptGet(ctx context.Context, req discorddata.TranscriptGetRequest) discorddata.TranscriptGetReply {
	row, found, err := h.repo.TranscriptGet(ctx, req.TicketID)
	if err != nil {
		message, code := failure(err)
		return discorddata.TranscriptGetReply{Error: message, Code: code}
	}
	if !found {
		return discorddata.TranscriptGetReply{}
	}
	return discorddata.TranscriptGetReply{
		Body:           row.Body,
		MessageCount:   row.MessageCount,
		StoredAtUnixMs: unixMs(row.StoredAt),
		Found:          true,
	}
}

// ticketView renders one stored row onto the wire type.
func ticketView(row *ent.Ticket) discorddata.Ticket {
	return discorddata.Ticket{
		ID:                row.ID,
		GuildID:           row.GuildID,
		ChannelID:         row.ChannelID,
		OpenerID:          row.OpenerID,
		Status:            string(row.Status),
		Subject:           row.Subject,
		ClaimedBy:         row.ClaimedBy,
		ClosedBy:          row.ClosedBy,
		ArchivedChannelID: row.ArchivedChannelID,
		PanelMessageID:    row.PanelMessageID,
		OpenedAtUnixMs:    unixMs(row.OpenedAt),
		ClaimedAtUnixMs:   unixMsPtr(row.ClaimedAt),
		ClosedAtUnixMs:    unixMsPtr(row.ClosedAt),
	}
}
