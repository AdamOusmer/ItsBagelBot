// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/users/ent/delegation"
	"ItsBagelBot/app/db/users/ent/predicate"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"

	"entgo.io/ent/dialect/sql"
)

func (r *Users) tallyDelegating(ctx context.Context, tally analytics.Tally, ids []uint64) (analytics.Tally, error) {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		q := r.client.User.Query().Where(user.TestAccount(false), user.Banned(false), predicate.User(hasLiveDelegate))
		if len(ids) != 0 {
			q.Where(user.IDIn(ids...))
		}
		return q.Count(ctx)
	})
	if err != nil {
		return nil, err
	}
	tally.AddN(int64(n), catalog.AccountsDelegating)
	return tally, nil
}

func hasLiveDelegate(s *sql.Selector) {
	grants := sql.Table(delegation.Table)
	s.Where(sql.Exists(sql.Select(grants.C(delegation.FieldOwnerID)).From(grants).Where(sql.And(
		sql.ColumnsEQ(grants.C(delegation.FieldOwnerID), s.C(user.FieldID)),
		sql.NotNull(grants.C(delegation.FieldConsumedAt)),
		sql.NEQ(grants.C(delegation.FieldDelegateID), 0),
	))))
}
