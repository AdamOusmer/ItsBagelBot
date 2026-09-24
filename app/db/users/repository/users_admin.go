// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/predicate"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

var ErrUserNotFound = errors.New("user not found")

const (
	AdminUserPageSize     = 15
	AdminUserMaxPages     = 25
	AdminUserMaxSearchLen = 200
)

const (
	userStatsKey            = "users:stats"
	userStatsCapacity int64 = 1
	userStatsTTL            = 60 * time.Second
)

func (r *Users) FindUser(ctx context.Context, userID uint64) (*ent.User, error) {
	u, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.IDEQ(userID)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, ErrUserNotFound
	}
	return u, err
}

func (r *Users) FindUserByUsername(ctx context.Context, username string) (*ent.User, error) {
	u, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.UsernameEqualFold(username)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, ErrUserNotFound
	}
	return u, err
}

type AdminUserQuery struct {
	Search string
	State  string
	Limit  int
	Offset int
}

func (r *Users) ListUsers(ctx context.Context, query AdminUserQuery) ([]*ent.User, error) {
	q := r.client.User.Query().Order(ent.Desc(user.FieldUpdatedAt), ent.Desc(user.FieldID))
	if s := NormalizeAdminSearch(query.Search); s != "" {
		q = q.Where(AdminSearchPredicate(s))
	}
	if pred, ok := AdminStatePredicate(query.State); ok {
		q = q.Where(pred)
	}
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return q.Offset(query.Offset).Limit(query.Limit).All(ctx)
	})
}

func AdminStatePredicate(state string) (predicate.User, bool) {
	switch state {
	case "banned":
		return user.BannedEQ(true), true
	case "inactive":
		return user.And(user.BannedEQ(false), user.IsActiveEQ(false)), true
	case "vip", "paid", "free":
		return user.And(
			user.BannedEQ(false),
			user.IsActiveEQ(true),
			user.StatusEQ(user.Status(state)),
		), true
	default:
		return nil, false
	}
}

type userStatsRow struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Paid   int `json:"paid"`
	Vip    int `json:"vip"`
}

func countIf(cond func(*entsql.Selector) string) ent.AggregateFunc {
	return func(s *entsql.Selector) string {
		return "COUNT(CASE WHEN " + cond(s) + " THEN 1 END)"
	}
}

// Safe only for the generated enum: do not widen it to take a plain string.
func statusIs(status user.Status) func(*entsql.Selector) string {
	return func(s *entsql.Selector) string {
		return s.C(user.FieldStatus) + " = '" + string(status) + "'"
	}
}

func isActiveCond(s *entsql.Selector) string {
	return s.C(user.FieldIsActive)
}

func (r *Users) UserStats(ctx context.Context) (total, active, paid, vip int, err error) {
	row, err := r.stats.GetOrLoad(ctx, userStatsKey, r.loadUserStats)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return row.Total, row.Active, row.Paid, row.Vip, nil
}

func (r *Users) loadUserStats(ctx context.Context) (userStatsRow, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (userStatsRow, error) {
		var rows []userStatsRow
		err := r.client.User.Query().
			Aggregate(
				ent.As(ent.Count(), "total"),
				ent.As(countIf(isActiveCond), "active"),
				ent.As(countIf(statusIs(user.StatusPaid)), "paid"),
				ent.As(countIf(statusIs(user.StatusVip)), "vip"),
			).
			Scan(ctx, &rows)
		if err != nil {
			return userStatsRow{}, err
		}
		if len(rows) == 0 {
			return userStatsRow{}, nil
		}
		return rows[0], nil
	})
}

type EnrollmentDay struct {
	Date  string
	Count int
}

func (r *Users) EnrollmentSeries(ctx context.Context, days int) ([]EnrollmentDay, error) {
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().
			Where(user.CreatedAtGTE(since)).
			Select(user.FieldCreatedAt).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int, days)
	for _, row := range rows {
		counts[row.CreatedAt.UTC().Format(time.DateOnly)]++
	}
	series := make([]EnrollmentDay, 0, days)
	for d := 0; d < days; d++ {
		date := since.AddDate(0, 0, d).Format(time.DateOnly)
		series = append(series, EnrollmentDay{Date: date, Count: counts[date]})
	}
	return series, nil
}

func (r *Users) HasToken(ctx context.Context, userID uint64, t tokens.Type, p tokens.Platform) (bool, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.client.Tokens.Query().
			Where(
				tokens.TypeEQ(t),
				tokens.PlatformEQ(p),
				tokens.HasUserWith(user.IDEQ(userID)),
			).
			Exist(ctx)
	})
}

func (r *Users) ClearToken(ctx context.Context, userID uint64, t tokens.Type, p tokens.Platform) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Tokens.Delete().
			Where(
				tokens.TypeEQ(t),
				tokens.PlatformEQ(p),
				tokens.HasUserWith(user.IDEQ(userID)),
			).
			Exec(ctx)
		return err
	})
}

func (r *Users) ResetTokens(ctx context.Context, userID uint64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Tokens.Delete().
			Where(tokens.HasUserWith(user.IDEQ(userID))).
			Exec(ctx)
		return err
	})
}

func NormalizeAdminSearch(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > AdminUserMaxSearchLen {
		return string(runes[:AdminUserMaxSearchLen])
	}
	return s
}

func AdminSearchPredicate(search string) predicate.User {
	preds := []predicate.User{
		user.UsernameContainsFold(search),
	}
	if id, err := strconv.ParseUint(search, 10, 64); err == nil {
		preds = append(preds, user.IDEQ(id))
	}
	return user.Or(preds...)
}
