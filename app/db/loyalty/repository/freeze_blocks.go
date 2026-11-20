// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

type freezeBlock struct {
	userID uint64
	kind   freezeBlockKind
	atMS   int64
}

type freezeBlockKind string

const (
	freezeTransfers freezeBlockKind = "block_transfer"
	freezeGames     freezeBlockKind = "close_games"
)

func newFreezeBlock(userID uint64, kind freezeBlockKind) freezeBlock {
	return freezeBlock{userID: userID, kind: kind, atMS: time.Now().UnixMilli()}
}

func (b freezeBlock) blockedQuery() string {
	return "SELECT 1 FROM loyalty_freezes WHERE user_id=? AND " + string(b.kind) + "=1 AND expires_at_ms>?"
}

func (b freezeBlock) allowed(s *entsql.Selector) {
	s.Where(entsql.NotExists(entsql.Expr(b.blockedQuery(), b.userID, b.atMS)))
}

func (b freezeBlock) refusal(ctx context.Context, r *Loyalty) error {
	_, err := db.WithRead(ctx, func(ctx context.Context) (int, error) {
		var blocked int
		err := r.sqldb.QueryRowContext(ctx, b.blockedQuery(), b.userID, b.atMS).Scan(&blocked)
		return blocked, err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.kind == freezeTransfers {
		return ErrFrozenTransfers
	}
	return ErrFrozenGames
}
