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

// ListPageSize is the default and maximum page size of TicketList. The desk
// and the dashboard both page; nothing needs a guild's whole history at once.
const ListPageSize = 50

// anyEmpty reports whether any identifier a verb requires is missing.
//
// Every verb here refuses on the same shape, a chain of `id == ""` tests
// joined by ||, and written out per verb that chain is five copies of one
// rule that a reader has to re-derive each time. Named once it also says what
// the rule is: these are required identifiers, not optional narrowings. The
// guild is one of them on purpose -- see liveTicket for what addressing a
// ticket by channel alone allowed.
func anyEmpty(ids ...string) bool {
	for _, id := range ids {
		if id == "" {
			return true
		}
	}
	return false
}

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
// The limit is counted over the opener's live rows held FOR UPDATE, so two
// simultaneous opens by one member serialize on those rows instead of both
// reading count == limit-1 and both inserting. It is still not an absolute
// invariant: the lock covers rows that exist, and MySQL under READ-COMMITTED
// takes no gap locks, so a member with no live ticket at all can still race
// two first opens past a limit of one. Closing that would need SERIALIZABLE
// for this path or a per-member lock row, and the cost of being wrong in that
// one case is one extra ticket channel a staff member closes. The invariant
// that does matter, one ticket per channel, is the unique index and is
// enforced by the database.
func (s *Store) TicketOpen(ctx context.Context, p OpenParams) (int, int, error) {
	if anyEmpty(p.GuildID, p.ChannelID, p.OpenerID) {
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

// openInTx is TicketOpen's body inside the transaction.
func (s *Store) openInTx(ctx context.Context, tx *ent.Tx, p OpenParams) (int, int, error) {
	count, err := s.lockedOpenCount(ctx, tx, p.GuildID, p.OpenerID)
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

// lockedOpenCount counts one member's live tickets in one guild, holding them
// for update. Claimed counts as open: a ticket a staff member is working is
// still one the opener holds.
//
// It selects the rows rather than issuing COUNT(*) because the point is the
// lock, and FOR UPDATE has nothing to attach to on an aggregate. The row set
// it walks is bounded by the guild's configured limit (1..5), so materializing
// it costs nothing. This mirrors XPAdd's lockedMember, including why SQLite --
// the enttest dialect -- skips the clause: it rejects FOR UPDATE outright and
// takes a file-wide write lock for the whole transaction instead, which is
// strictly stronger. See Store.rowLocks.
func (s *Store) lockedOpenCount(ctx context.Context, tx *ent.Tx, guildID, openerID string) (int, error) {
	query := tx.Ticket.Query().
		Where(
			ticket.GuildIDEQ(guildID),
			ticket.OpenerIDEQ(openerID),
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

// TicketOpenCount is openCount outside a transaction: the desk asks before it
// creates a channel, both to refuse an over-limit open without a wasted REST
// round trip and to number the channel it is about to create. The answer is
// advisory for the same reason openInTx's check is (see TicketOpen).
func (s *Store) TicketOpenCount(ctx context.Context, guildID, openerID string) (int, error) {
	if anyEmpty(guildID, openerID) {
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
	if anyEmpty(guildID, channelID, staffID) {
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
	if anyEmpty(p.GuildID, p.ChannelID) {
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

// closedStatus picks the terminal status from whether the channel survived.
func closedStatus(archivedChannelID string) ticket.Status {
	if archivedChannelID != "" {
		return ticket.StatusArchived
	}
	return ticket.StatusClosed
}

// liveTicket loads a ticket by channel within one guild, mapping absence (and
// a channel that belongs to a different guild) onto ErrNotFound.
//
// The guild is mandatory rather than an optional narrowing. It used to be
// optional because a Discord channel snowflake is globally unique, which makes
// the filter redundant for a well-formed caller -- but it is exactly the
// filter that stops a caller who reached this RPC with a channel id from
// another guild from claiming or closing that guild's ticket. Every verb now
// carries the guild (the engine has it on c.Config.GuildID at every call
// site), so nothing is left needing the loose form.
func liveTicket(ctx context.Context, tx *ent.Tx, guildID, channelID string) (*ent.Ticket, error) {
	row, err := tx.Ticket.Query().
		Where(ticket.ChannelIDEQ(channelID), ticket.GuildIDEQ(guildID)).
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
	if anyEmpty(guildID, channelID) {
		return nil, false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Ticket, error) {
		return s.client.Ticket.Query().
			Where(ticket.ChannelIDEQ(channelID), ticket.GuildIDEQ(guildID)).
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
