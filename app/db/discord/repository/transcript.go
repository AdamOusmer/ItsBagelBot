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

const MaxTranscriptBytes = 2 << 20

func (s *Store) TranscriptPut(ctx context.Context, ticketID int, body string, messageCount int) error {
	if ticketID <= 0 {
		return ErrInvalidInput
	}
	if len(body) > MaxTranscriptBytes {
		return ErrInvalidInput
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			return putTranscriptInTx(ctx, tx, transcriptPut{
				ticketID:     ticketID,
				body:         body,
				messageCount: messageCount,
			})
		})
	})
}

type transcriptPut struct {
	ticketID     int
	body         string
	messageCount int
}

func putTranscriptInTx(ctx context.Context, tx *ent.Tx, p transcriptPut) error {
	exists, err := tx.Ticket.Query().Where(ticket.IDEQ(p.ticketID)).Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	existing, err := tx.TicketTranscript.Query().
		Where(tickettranscript.HasTicketWith(ticket.IDEQ(p.ticketID))).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if existing != nil {
		return tx.TicketTranscript.UpdateOne(existing).
			SetBody(p.body).
			SetMessageCount(p.messageCount).
			Exec(ctx)
	}
	return tx.TicketTranscript.Create().
		SetTicketID(p.ticketID).
		SetBody(p.body).
		SetMessageCount(p.messageCount).
		Exec(ctx)
}

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
