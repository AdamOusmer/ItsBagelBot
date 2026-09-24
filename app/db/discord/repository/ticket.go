// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/ticket"
	"ItsBagelBot/pkg/db"
)

const ListPageSize = 50

type TicketKey struct {
	GuildID   string
	ChannelID string
}

func (k TicketKey) complete() bool {
	return k.GuildID != "" && k.ChannelID != ""
}

type MemberKey struct {
	GuildID  string
	MemberID string
}

func (m MemberKey) complete() bool {
	return m.GuildID != "" && m.MemberID != ""
}

type OpenParams struct {
	Key            TicketKey
	OpenerID       string
	Subject        string
	PanelMessageID string
	Limit          int
}

func (p OpenParams) member() MemberKey {
	return MemberKey{GuildID: p.Key.GuildID, MemberID: p.OpenerID}
}

func (s *Store) TicketOpen(ctx context.Context, p OpenParams) (int, int, error) {
	if !p.Key.complete() || p.OpenerID == "" {
		return 0, 0, ErrInvalidInput
	}
	var id, count int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			var openErr error
			id, count, openErr = s.openInTx(ctx, tx, p)
			return openErr
		})
	})
	if err != nil {
		return 0, count, err
	}
	return id, count, nil
}

func (s *Store) openInTx(ctx context.Context, tx *ent.Tx, p OpenParams) (int, int, error) {
	count, err := s.lockedOpenCount(ctx, tx, p.member())
	if err != nil {
		return 0, 0, err
	}

	existing, err := tx.Ticket.Query().Where(ticket.ChannelIDEQ(p.Key.ChannelID)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return 0, count, err
	}
	if existing != nil {
		return existing.ID, count, nil
	}

	if p.Limit > 0 && count >= p.Limit {
		return 0, count, ErrOpenLimit
	}

	row, err := tx.Ticket.Create().
		SetGuildID(p.Key.GuildID).
		SetChannelID(p.Key.ChannelID).
		SetOpenerID(p.OpenerID).
		SetSubject(p.Subject).
		SetPanelMessageID(p.PanelMessageID).
		SetStatus(ticket.StatusOpen).
		Save(ctx)
	if err != nil {
		return 0, count, err
	}
	return row.ID, count + 1, nil
}

func (s *Store) lockedOpenCount(ctx context.Context, tx *ent.Tx, m MemberKey) (int, error) {
	query := tx.Ticket.Query().
		Where(
			ticket.GuildIDEQ(m.GuildID),
			ticket.OpenerIDEQ(m.MemberID),
			ticket.StatusIn(ticket.StatusOpen, ticket.StatusClaimed),
		)
	if s.rowLocks {
		query = query.ForUpdate()
	}
	rows, err := query.All(ctx)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *Store) TicketOpenCount(ctx context.Context, m MemberKey) (int, error) {
	if !m.complete() {
		return 0, ErrInvalidInput
	}
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return s.client.Ticket.Query().
			Where(
				ticket.GuildIDEQ(m.GuildID),
				ticket.OpenerIDEQ(m.MemberID),
				ticket.StatusIn(ticket.StatusOpen, ticket.StatusClaimed),
			).
			Count(ctx)
	})
}

type ClaimParams struct {
	Key     TicketKey
	StaffID string
}

func (s *Store) TicketClaim(ctx context.Context, p ClaimParams) (int, error) {
	if !p.Key.complete() || p.StaffID == "" {
		return 0, ErrInvalidInput
	}
	var id int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			row, err := liveTicket(ctx, tx, p.Key)
			if err != nil {
				return err
			}
			if row.Status != ticket.StatusOpen && row.Status != ticket.StatusClaimed {
				return ErrInvalidInput
			}
			id = row.ID
			update := tx.Ticket.UpdateOne(row).SetStatus(ticket.StatusClaimed).SetClaimedBy(p.StaffID)
			if row.ClaimedAt == nil {
				update = update.SetClaimedAt(time.Now())
			}
			return update.Exec(ctx)
		})
	})
	return id, err
}

type CloseParams struct {
	Key               TicketKey
	ClosedBy          string
	ArchivedChannelID string
}

func (s *Store) TicketClose(ctx context.Context, p CloseParams) (int, string, error) {
	if !p.Key.complete() {
		return 0, "", ErrInvalidInput
	}
	var id int
	var openerID string
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			row, err := liveTicket(ctx, tx, p.Key)
			if err != nil {
				return err
			}
			id, openerID = row.ID, row.OpenerID
			if row.Status == ticket.StatusClosed || row.Status == ticket.StatusArchived {
				return nil
			}
			return tx.Ticket.UpdateOne(row).
				SetStatus(closedStatus(p)).
				SetClosedBy(p.ClosedBy).
				SetArchivedChannelID(p.ArchivedChannelID).
				SetClosedAt(time.Now()).
				Exec(ctx)
		})
	})
	return id, openerID, err
}

func closedStatus(p CloseParams) ticket.Status {
	if p.ArchivedChannelID != "" {
		return ticket.StatusArchived
	}
	return ticket.StatusClosed
}

// Filtering by guild stops a caller acting on another guild's ticket by channel id.
func liveTicket(ctx context.Context, tx *ent.Tx, k TicketKey) (*ent.Ticket, error) {
	row, err := tx.Ticket.Query().
		Where(ticket.ChannelIDEQ(k.ChannelID), ticket.GuildIDEQ(k.GuildID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) TicketGet(ctx context.Context, k TicketKey) (*ent.Ticket, bool, error) {
	if !k.complete() {
		return nil, false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Ticket, error) {
		return s.client.Ticket.Query().
			Where(ticket.ChannelIDEQ(k.ChannelID), ticket.GuildIDEQ(k.GuildID)).
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

type ListParams struct {
	GuildID string
	Status  string
	Limit   int
	Cursor  string
}

func (s *Store) TicketList(ctx context.Context, p ListParams) ([]*ent.Ticket, string, error) {
	if p.GuildID == "" {
		return nil, "", ErrInvalidInput
	}
	limit := p.Limit
	if limit <= 0 || limit > ListPageSize {
		limit = ListPageSize
	}
	query, err := s.listQuery(p)
	if err != nil {
		return nil, "", err
	}

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Ticket, error) {
		return query.Order(ent.Desc(ticket.FieldID)).Limit(limit + 1).All(ctx)
	})
	if err != nil {
		return nil, "", err
	}
	if len(rows) <= limit {
		return rows, "", nil
	}
	rows = rows[:limit]
	return rows, strconv.Itoa(rows[limit-1].ID), nil
}

func (s *Store) listQuery(p ListParams) (*ent.TicketQuery, error) {
	query := s.client.Ticket.Query().Where(ticket.GuildIDEQ(p.GuildID))
	if p.Status != "" {
		status := ticket.Status(p.Status)
		if err := ticket.StatusValidator(status); err != nil {
			return nil, ErrInvalidInput
		}
		query = query.Where(ticket.StatusEQ(status))
	}
	if p.Cursor == "" {
		return query, nil
	}
	after, err := strconv.Atoi(p.Cursor)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return query.Where(ticket.IDLT(after)), nil
}
