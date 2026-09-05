// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/memberxp"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/db"
)

// DailyWindow is how long a member must wait between daily claims. It is
// measured from the stored last_daily inside the same transaction that writes
// the new one, which is what makes two simultaneous /daily calls award the
// bonus exactly once -- the Valkey key it replaces was a 24h TTL SET NX, which
// had the same property but lost the claim on an eviction.
const DailyWindow = 24 * time.Hour

// TopLimit is the default and maximum leaderboard page size.
const TopLimit = 25

// XPGet reads one member's standing. A member who has never earned XP is
// (nil, false, nil): zero XP at level 0 is what the caller renders either way.
func (s *Store) XPGet(ctx context.Context, guildID, userID string) (*ent.MemberXP, bool, error) {
	if guildID == "" || userID == "" {
		return nil, false, ErrInvalidInput
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.MemberXP, error) {
		return s.client.MemberXP.Query().
			Where(memberxp.GuildIDEQ(guildID), memberxp.UserIDEQ(userID)).
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

// XPResult is one member's standing after a write.
type XPResult struct {
	XP    int64
	Level int
	// LeveledUp is true only when this write crossed a level boundary, so the
	// caller announces a level-up exactly once.
	LeveledUp bool
	// Granted and NextDaily are meaningful for XPDaily only.
	Granted   bool
	NextDaily time.Time
}

// XPAdd credits delta XP to one member and reports the new standing. The 60s
// per-message cooldown that decides whether a message earns XP at all stays in
// Valkey on the engine side; by the time a request reaches here the award is
// already decided.
func (s *Store) XPAdd(ctx context.Context, guildID, userID string, delta int64) (XPResult, error) {
	if guildID == "" || userID == "" {
		return XPResult{}, ErrInvalidInput
	}
	var out XPResult
	err := s.retryOnConflict(ctx, func(ctx context.Context, tx *ent.Tx) error {
		row, err := s.lockedMember(ctx, tx, guildID, userID)
		if err != nil {
			return err
		}
		before := storedXP(row)
		out = xpResultFor(before, before+delta)
		return writeXP(ctx, tx, memberRef{GuildID: guildID, UserID: userID, Row: row}, xpWrite{
			XP:    out.XP,
			Level: out.Level,
		})
	})
	if err != nil {
		return XPResult{}, err
	}
	return out, nil
}

// XPDaily claims one member's daily bonus. Granted is false when the member is
// still inside DailyWindow, and NextDaily then says when they may claim again.
func (s *Store) XPDaily(ctx context.Context, guildID, userID string, amount int64) (XPResult, error) {
	if guildID == "" || userID == "" {
		return XPResult{}, ErrInvalidInput
	}
	var out XPResult
	err := s.retryOnConflict(ctx, func(ctx context.Context, tx *ent.Tx) error {
		row, err := s.lockedMember(ctx, tx, guildID, userID)
		if err != nil {
			return err
		}
		now := time.Now()
		if next, waiting := dailyPending(row, now); waiting {
			out = XPResult{XP: storedXP(row), Level: ddiscord.LevelOf(storedXP(row)), NextDaily: next}
			return nil
		}
		before := storedXP(row)
		out = xpResultFor(before, before+amount)
		out.Granted = true
		out.NextDaily = now.Add(DailyWindow)
		return writeXP(ctx, tx, memberRef{GuildID: guildID, UserID: userID, Row: row}, xpWrite{
			XP:    out.XP,
			Level: out.Level,
			Daily: &now,
		})
	})
	if err != nil {
		return XPResult{}, err
	}
	return out, nil
}

// XPTop returns one guild's leaderboard, highest first.
func (s *Store) XPTop(ctx context.Context, guildID string, limit int) ([]*ent.MemberXP, error) {
	if guildID == "" {
		return nil, ErrInvalidInput
	}
	if limit <= 0 || limit > TopLimit {
		limit = TopLimit
	}
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.MemberXP, error) {
		return s.client.MemberXP.Query().
			Where(memberxp.GuildIDEQ(guildID)).
			// user_id breaks the tie so a page boundary is stable when several
			// members sit on the same total.
			Order(ent.Desc(memberxp.FieldXp), ent.Asc(memberxp.FieldUserID)).
			Limit(limit).
			All(ctx)
	})
}

// dailyPending reports whether the member is still inside the daily window and,
// if so, when it opens again.
func dailyPending(row *ent.MemberXP, now time.Time) (time.Time, bool) {
	if row == nil || row.LastDaily == nil {
		return time.Time{}, false
	}
	next := row.LastDaily.Add(DailyWindow)
	if now.Before(next) {
		return next, true
	}
	return time.Time{}, false
}

// storedXP reads a possibly-absent row's total.
func storedXP(row *ent.MemberXP) int64 {
	if row == nil {
		return 0
	}
	return row.Xp
}

// xpResultFor derives the standing after a total moves from before to after,
// including whether the move crossed a level boundary.
func xpResultFor(before, after int64) XPResult {
	level := ddiscord.LevelOf(after)
	return XPResult{XP: after, Level: level, LeveledUp: level > ddiscord.LevelOf(before)}
}

// memberRef addresses one member row, carrying the already-loaded row when
// there is one so writeXP does not re-read it.
type memberRef struct {
	GuildID string
	UserID  string
	Row     *ent.MemberXP
}

// xpWrite is the new state of one member row. Daily is nil to leave last_daily
// untouched.
type xpWrite struct {
	XP    int64
	Level int
	Daily *time.Time
}

// writeXP persists w, creating the row when the member has none yet.
func writeXP(ctx context.Context, tx *ent.Tx, ref memberRef, w xpWrite) error {
	if ref.Row == nil {
		create := tx.MemberXP.Create().
			SetGuildID(ref.GuildID).
			SetUserID(ref.UserID).
			SetXp(w.XP).
			SetLevel(w.Level)
		if w.Daily != nil {
			create = create.SetLastDaily(*w.Daily)
		}
		return create.Exec(ctx)
	}
	update := tx.MemberXP.UpdateOne(ref.Row).SetXp(w.XP).SetLevel(w.Level)
	if w.Daily != nil {
		update = update.SetLastDaily(*w.Daily)
	}
	return update.Exec(ctx)
}

// lockedMember loads one member row for update, or nil when the member has
// none yet. The row lock serializes concurrent writers on MySQL; see Store's
// rowLocks note for why SQLite skips it.
func (s *Store) lockedMember(ctx context.Context, tx *ent.Tx, guildID, userID string) (*ent.MemberXP, error) {
	query := tx.MemberXP.Query().Where(memberxp.GuildIDEQ(guildID), memberxp.UserIDEQ(userID))
	if s.rowLocks {
		query = query.ForUpdate()
	}
	row, err := query.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// retryOnConflict runs fn in a transaction, once more on a unique-constraint
// error.
//
// The read-modify-write paths take a row lock, but SELECT ... FOR UPDATE locks
// nothing when the row does not exist yet: MySQL under READ-COMMITTED takes no
// gap locks, so two first-ever writes for the same member both see no row and
// both insert. One loses on the unique index. Retrying finds the winner's row
// and takes the update branch, which is correct and additive. One retry is
// enough by construction -- after it the row exists, and the lock does apply.
func (s *Store) retryOnConflict(ctx context.Context, fn func(context.Context, *ent.Tx) error) error {
	run := func(ctx context.Context) error {
		return withTx(ctx, s.client, func(tx *ent.Tx) error { return fn(ctx, tx) })
	}
	err := db.WithExec(ctx, run)
	if !ent.IsConstraintError(err) {
		return err
	}
	return db.WithExec(ctx, run)
}
