// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/ticket"
	"ItsBagelBot/app/db/discord/ent/tickettranscript"
	"ItsBagelBot/pkg/db"
)

// MaxTranscriptBytes caps one stored transcript.
//
// The engine paginates a closing ticket's history to 2000 messages; at the
// ~1 KiB a long Discord message plus attachment URLs renders to, that lands
// just under 2 MiB. The cap exists because this column rides the nightly
// logical mysqldump of every schema: an unbounded transcript would make the
// backup grow with chat volume rather than with the dataset. Refusing is the
// right failure -- a truncated transcript that silently loses the end of a
// support conversation is worse than a close that logs a refusal and keeps
// the channel.
const MaxTranscriptBytes = 2 << 20

// TranscriptPut stores (or replaces) one ticket's rendered transcript.
// Replacing rather than appending: the engine renders the whole history in one
// pass at close time, so a second put is a retry of the same render.
func (s *Store) TranscriptPut(ctx context.Context, ticketID int, body string, messageCount int) error {
	if ticketID <= 0 {
		return ErrInvalidInput
	}
	if len(body) > MaxTranscriptBytes {
		return ErrInvalidInput
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			return putTranscriptInTx(ctx, tx, ticketID, body, messageCount)
		})
	})
}

// putTranscriptInTx is TranscriptPut's body inside the transaction.
func putTranscriptInTx(ctx context.Context, tx *ent.Tx, ticketID int, body string, messageCount int) error {
	exists, err := tx.Ticket.Query().Where(ticket.IDEQ(ticketID)).Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	existing, err := tx.TicketTranscript.Query().
		Where(tickettranscript.HasTicketWith(ticket.IDEQ(ticketID))).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if existing != nil {
		return tx.TicketTranscript.UpdateOne(existing).
			SetBody(body).
			SetMessageCount(messageCount).
			Exec(ctx)
	}
	return tx.TicketTranscript.Create().
		SetTicketID(ticketID).
		SetBody(body).
		SetMessageCount(messageCount).
		Exec(ctx)
}

// TranscriptGet reads one ticket's transcript back. A ticket with no stored
// transcript is (nil, false, nil) -- transcripts are off by config for some
// guilds, so absence is ordinary.
func (s *Store) TranscriptGet(ctx context.Context, ticketID int) (*ent.TicketTranscript, bool, error) {
	if ticketID <= 0 {
		return nil, false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.TicketTranscript, error) {
		return s.client.TicketTranscript.Query().
			Where(tickettranscript.HasTicketWith(ticket.IDEQ(ticketID))).
			Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return row, true, nil
}
