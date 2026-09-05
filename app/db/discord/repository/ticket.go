// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"
	"time"

	"entgo.io/ent/dialect/sql"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/predicate"
	"ItsBagelBot/app/db/discord/ent/ticket"
	"ItsBagelBot/pkg/db"
)

// ListPageSize is the default and maximum page size of TicketList. The desk
// and the dashboard both page; nothing needs a guild's whole history at once.
const ListPageSize = 50

// OpenParams describes one ticket channel the engine has just created.
type OpenParams struct {
	GuildID   string
	ChannelID string
	OpenerID  string
	Subject   string
	// PanelMessageID is the "Ticket" card the engine posted into the channel
	// just before recording the row. It arrives at open time rather than in a
	// later update because the engine has both ids by then (outgress creates
	// the channel and posts the card in one round trip) and a second write
	// would be a second transaction for a value that never changes.
	PanelMessageID string
	// Limit is the guild's configured per-member cap on open tickets. Zero
	// means unlimited. It arrives in the request rather than being read here:
	// the config lives in the modules blob, and this service deliberately owns
	// no config of its own.
	Limit int
}

// TicketOpen records a new ticket and returns its id together with the
// opener's resulting open count. Replaying the same channel is idempotent and
// returns the existing row.
//
// The limit check is advisory, not an invariant. Under READ-COMMITTED MySQL
// takes no gap locks, so two simultaneous opens by one member can both see
// count == limit-1 and both insert. Making it exact would need either
// SERIALIZABLE for this path or a per-member lock row, and the cost of being
// wrong is one extra ticket channel a staff member closes -- far below the cost
// of either. The invariant that does matter, one ticket per channel, is the
// unique index and is enforced by the database.
func (s *Store) TicketOpen(ctx context.Context, p OpenParams) (int, int, error) {
	if p.GuildID == "" || p.ChannelID == "" || p.OpenerID == "" {
		return 0, 0, ErrInvalidInput
	}
	var id, count int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			var openErr error
			id, count, openErr = openInTx(ctx, tx, p)
			return openErr
		})
	})
	if err != nil {
		return 0, count, err
	}
	return id, count, nil
}

// openInTx is TicketOpen's body inside the transaction.
func openInTx(ctx context.Context, tx *ent.Tx, p OpenParams) (int, int, error) {
	count, err := openCount(ctx, tx, p.GuildID, p.OpenerID)
	if err != nil {
		return 0, 0, err
	}

	existing, err := tx.Ticket.Query().Where(ticket.ChannelIDEQ(p.ChannelID)).Only(ctx)
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
		SetGuildID(p.GuildID).
		SetChannelID(p.ChannelID).
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

// openCount counts one member's live tickets in one guild. Claimed counts as
// open: a ticket a staff member is working is still one the opener holds.
func openCount(ctx context.Context, tx *ent.Tx, guildID, openerID string) (int, error) {
	return tx.Ticket.Query().
		Where(
			ticket.GuildIDEQ(guildID),
			ticket.OpenerIDEQ(openerID),
			ticket.StatusIn(ticket.StatusOpen, ticket.StatusClaimed),
		).
		Count(ctx)
}

// TicketOpenCount is openCount outside a transaction: the desk asks before it
// creates a channel, both to refuse an over-limit open without a wasted REST
// round trip and to number the channel it is about to create. The answer is
// advisory for the same reason openInTx's check is (see TicketOpen).
func (s *Store) TicketOpenCount(ctx context.Context, guildID, openerID string) (int, error) {
	if guildID == "" || openerID == "" {
		return 0, ErrInvalidInput
	}
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return s.client.Ticket.Query().
			Where(
				ticket.GuildIDEQ(guildID),
				ticket.OpenerIDEQ(openerID),
				ticket.StatusIn(ticket.StatusOpen, ticket.StatusClaimed),
			).
			Count(ctx)
	})
}

// TicketClaim marks the ticket in channelID as claimed by staffID. A second
// claim by a different staff member moves the name over but keeps the original
// claimed_at, so the desk's "claimed N minutes ago" stays honest. Claiming a
// closed or archived ticket is ErrInvalidInput; there is no such transition.
func (s *Store) TicketClaim(ctx context.Context, guildID, channelID, staffID string) (int, error) {
	if channelID == "" || staffID == "" {
		return 0, ErrInvalidInput
	}
	var id int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			row, err := liveTicket(ctx, tx, guildID, channelID)
			if err != nil {
				return err
			}
			if row.Status != ticket.StatusOpen && row.Status != ticket.StatusClaimed {
				return ErrInvalidInput
			}
			id = row.ID
			update := tx.Ticket.UpdateOne(row).SetStatus(ticket.StatusClaimed).SetClaimedBy(staffID)
			if row.ClaimedAt == nil {
				update = update.SetClaimedAt(time.Now())
			}
			return update.Exec(ctx)
		})
	})
	return id, err
}

// CloseParams describes one ticket being closed. A non-empty
// ArchivedChannelID means the channel was moved into the archive category
// instead of being deleted, and the row lands in status archived.
type CloseParams struct {
	GuildID           string
	ChannelID         string
	ClosedBy          string
	ArchivedChannelID string
}

// TicketClose closes a ticket and returns its id and opener, so the caller can
// address the close summary without a second lookup. Closing an
// already-closed ticket is a no-op that still returns those two: the engine
// retries a close whenever a button press and a slash command race, and a
// second write would move closed_at and corrupt the recorded duration.
func (s *Store) TicketClose(ctx context.Context, p CloseParams) (int, string, error) {
	if p.ChannelID == "" {
		return 0, "", ErrInvalidInput
	}
	var id int
	var openerID string
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error {
			row, err := liveTicket(ctx, tx, p.GuildID, p.ChannelID)
			if err != nil {
				return err
			}
			id, openerID = row.ID, row.OpenerID
			if row.Status == ticket.StatusClosed || row.Status == ticket.StatusArchived {
				return nil
			}
			return tx.Ticket.UpdateOne(row).
				SetStatus(closedStatus(p.ArchivedChannelID)).
				SetClosedBy(p.ClosedBy).
				SetArchivedChannelID(p.ArchivedChannelID).
				SetClosedAt(time.Now()).
				Exec(ctx)
		})
	})
	return id, openerID, err
}

// optionalGuild scopes a channel lookup to one guild, or to any guild when the
// caller did not name one. See liveTicket for why that is safe.
func optionalGuild(guildID string) predicate.Ticket {
	if guildID == "" {
		return func(*sql.Selector) {}
	}
	return ticket.GuildIDEQ(guildID)
}

// closedStatus picks the terminal status from whether the channel survived.
func closedStatus(archivedChannelID string) ticket.Status {
	if archivedChannelID != "" {
		return ticket.StatusArchived
	}
	return ticket.StatusClosed
}

// liveTicket loads a ticket by channel, mapping absence (and a channel that
// belongs to a different guild) onto ErrNotFound.
//
// An empty guildID matches any guild. A Discord channel snowflake is globally
// unique, so the guild is a defence-in-depth filter rather than part of the
// key, and the legacy Store verbs the engine still calls address a ticket by
// channel alone.
func liveTicket(ctx context.Context, tx *ent.Tx, guildID, channelID string) (*ent.Ticket, error) {
	row, err := tx.Ticket.Query().
		Where(ticket.ChannelIDEQ(channelID), optionalGuild(guildID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// TicketGet resolves a ticket from the channel a button was pressed in. A
// channel with no ticket is (nil, false, nil).
func (s *Store) TicketGet(ctx context.Context, guildID, channelID string) (*ent.Ticket, bool, error) {
	if channelID == "" {
		return nil, false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Ticket, error) {
		return s.client.Ticket.Query().
			Where(ticket.ChannelIDEQ(channelID), optionalGuild(guildID)).
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

// ListParams pages one guild's tickets, newest first.
type ListParams struct {
	GuildID string
	// Status is empty for every status, or one ticket.Status value.
	Status string
	Limit  int
	// Cursor is the previous page's NextCursor (an opaque ticket id). Empty
	// starts at the newest row.
	Cursor string
}

// TicketList returns one page of a guild's tickets, newest first, plus the
// cursor for the next page (empty on the last one). Paging is keyset, on the
// autoincrement id, rather than OFFSET: a ticket opened while an operator is
// paging must not shift every later row by one.
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

	// One row over the page size: its presence is what says another page
	// exists, without a second COUNT over the same predicate.
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

// listQuery builds TicketList's predicate: the guild, an optional status, and
// the keyset cursor.
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
