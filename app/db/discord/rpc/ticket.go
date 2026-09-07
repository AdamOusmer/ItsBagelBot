// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/internal/domain/rpc/discorddata"
)

type ticketRPC struct{ repo TicketStore }

// subscribeTickets registers the ticket-desk and transcript verbs.
func subscribeTickets(w Wiring) error {
	h := ticketRPC{repo: w.Repo}
	return errors.Join(
		serve(w, discorddata.VerbTicketOpen, h.open),
		serve(w, discorddata.VerbTicketClaim, h.claim),
		serve(w, discorddata.VerbTicketClose, h.close),
		serve(w, discorddata.VerbTicketGet, h.get),
		serve(w, discorddata.VerbTicketCount, h.count),
		serve(w, discorddata.VerbTicketList, h.list),
		subscribeTranscripts(w, h),
	)
}

// subscribeTranscripts registers the two transcript verbs.
func subscribeTranscripts(w Wiring, h ticketRPC) error {
	return errors.Join(
		serve(w, discorddata.VerbTranscriptPut, h.transcriptPut),
		serve(w, discorddata.VerbTranscriptGet, h.transcriptGet),
	)
}

func (h ticketRPC) open(ctx context.Context, req discorddata.TicketOpenRequest) discorddata.TicketOpenReply {
	id, count, err := h.repo.TicketOpen(ctx, repository.OpenParams{
		Key:      ticketKey(req.GuildID, req.ChannelID),
		OpenerID: req.OpenerID,
		Subject:  req.Subject,
		Limit:    req.OpenLimit,

		PanelMessageID: req.PanelMessageID,
	})
	// The count travels even on a refusal: at CodeLimit it is how many
	// tickets the opener already holds, which is what the ephemeral reply
	// names back to them. So this one verb does not go through reply().
	if err != nil {
		return discorddata.TicketOpenReply{OpenCount: count, Refusal: refusal(err)}
	}
	return discorddata.TicketOpenReply{TicketID: id, OpenCount: count}
}

func (h ticketRPC) claim(ctx context.Context, req discorddata.TicketClaimRequest) discorddata.TicketClaimReply {
	id, err := h.repo.TicketClaim(ctx, repository.ClaimParams{
		Key:     ticketKey(req.GuildID, req.ChannelID),
		StaffID: req.StaffID,
	})
	return reply(err, func() discorddata.TicketClaimReply { return discorddata.TicketClaimReply{TicketID: id} })
}

func (h ticketRPC) close(ctx context.Context, req discorddata.TicketCloseRequest) discorddata.TicketCloseReply {
	id, openerID, err := h.repo.TicketClose(ctx, repository.CloseParams{
		Key:               ticketKey(req.GuildID, req.ChannelID),
		ClosedBy:          req.ClosedBy,
		ArchivedChannelID: req.ArchivedChannelID,
	})
	return reply(err, func() discorddata.TicketCloseReply {
		return discorddata.TicketCloseReply{TicketID: id, OpenerID: openerID}
	})
}

func (h ticketRPC) get(ctx context.Context, req discorddata.TicketGetRequest) discorddata.TicketGetReply {
	row, found, err := h.repo.TicketGet(ctx, ticketKey(req.GuildID, req.ChannelID))
	return lookup(found, err, func() discorddata.TicketGetReply {
		return discorddata.TicketGetReply{Ticket: ticketView(row), Found: true}
	})
}

func (h ticketRPC) count(ctx context.Context, req discorddata.TicketCountRequest) discorddata.TicketCountReply {
	n, err := h.repo.TicketOpenCount(ctx, repository.MemberKey{GuildID: req.GuildID, MemberID: req.OpenerID})
	return reply(err, func() discorddata.TicketCountReply { return discorddata.TicketCountReply{Count: n} })
}

func (h ticketRPC) list(ctx context.Context, req discorddata.TicketListRequest) discorddata.TicketListReply {
	rows, next, err := h.repo.TicketList(ctx, repository.ListParams{
		GuildID: req.GuildID,
		Status:  req.Status,
		Limit:   req.Limit,
		Cursor:  req.Cursor,
	})
	return reply(err, func() discorddata.TicketListReply {
		views := make([]discorddata.Ticket, 0, len(rows))
		for _, row := range rows {
			views = append(views, ticketView(row))
		}
		return discorddata.TicketListReply{Tickets: views, NextCursor: next}
	})
}

func (h ticketRPC) transcriptPut(ctx context.Context, req discorddata.TranscriptPutRequest) discorddata.TranscriptPutReply {
	err := h.repo.TranscriptPut(ctx, req.TicketID, req.Body, req.MessageCount)
	return discorddata.TranscriptPutReply{Refusal: refusal(err)}
}

func (h ticketRPC) transcriptGet(ctx context.Context, req discorddata.TranscriptGetRequest) discorddata.TranscriptGetReply {
	row, found, err := h.repo.TranscriptGet(ctx, req.TicketID)
	return lookup(found, err, func() discorddata.TranscriptGetReply {
		return discorddata.TranscriptGetReply{
			Body:           row.Body,
			MessageCount:   row.MessageCount,
			StoredAtUnixMs: unixMs(row.StoredAt),
			Found:          true,
		}
	})
}

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

func ticketKey(guildID, channelID string) repository.TicketKey {
	return repository.TicketKey{GuildID: guildID, ChannelID: channelID}
}
