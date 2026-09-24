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

const DailyWindow = 24 * time.Hour

const TopLimit = 25

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

type XPResult struct {
	XP        int64
	Level     int
	LeveledUp bool
	Granted   bool
	NextDaily time.Time
}

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
			Order(ent.Desc(memberxp.FieldXp), ent.Asc(memberxp.FieldUserID)).
			Limit(limit).
			All(ctx)
	})
}

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

func storedXP(row *ent.MemberXP) int64 {
	if row == nil {
		return 0
	}
	return row.Xp
}

func xpResultFor(before, after int64) XPResult {
	level := ddiscord.LevelOf(after)
	return XPResult{XP: after, Level: level, LeveledUp: level > ddiscord.LevelOf(before)}
}

type memberRef struct {
	GuildID string
	UserID  string
	Row     *ent.MemberXP
}

type xpWrite struct {
	XP    int64
	Level int
	Daily *time.Time
}

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
