// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
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
	// AdminLiveIDCap bounds the IN list for the live filter. The console's
	// live set is the same live:<id> scan the timer reconciler already walks
	// once a minute, which is hundreds of channels, not tens of thousands. Past
	// this the caller is handed an error instead of a truncated page: a short
	// IN list would look like a complete answer.
	AdminLiveIDCap = 10_000
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

const AdminOrderOldest = "oldest"

type AdminUserQuery struct {
	Search string
	State  string
	Order  string
	Limit  int
	Offset int
	// OnlyLive keeps rows whose id is in LiveIDs. Empty matches nobody.
	// LiveIDs is not consulted when OnlyLive is false.
	OnlyLive bool
	LiveIDs  []uint64
}

func (r *Users) ListUsers(ctx context.Context, query AdminUserQuery) ([]*ent.User, error) {
	q, err := adminDirectoryQuery(r.client, query)
	if err != nil {
		return nil, err
	}
	if q == nil {
		return nil, nil
	}
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return q.Offset(query.Offset).Limit(query.Limit).All(ctx)
	})
}

func adminDirectoryQuery(client *ent.Client, query AdminUserQuery) (*ent.UserQuery, error) {
	if err := validateLiveIDs(query); err != nil {
		return nil, err
	}
	if liveFilterEmpty(query) {
		return nil, nil
	}
	return withAdminFilters(client.User.Query(), query), nil
}

func validateLiveIDs(query AdminUserQuery) error {
	if !query.OnlyLive {
		return nil
	}
	if len(query.LiveIDs) <= AdminLiveIDCap {
		return nil
	}
	return fmt.Errorf("live id list exceeds %d", AdminLiveIDCap)
}

func liveFilterEmpty(query AdminUserQuery) bool {
	if !query.OnlyLive {
		return false
	}
	return len(query.LiveIDs) == 0
}

func withAdminFilters(q *ent.UserQuery, query AdminUserQuery) *ent.UserQuery {
	by := adminOrder(query)
	q = q.Order(by(user.FieldCreatedAt), by(user.FieldID))
	q = whereAdminSearch(q, query.Search)
	q = whereAdminState(q, query.State)
	return whereAdminLive(q, query)
}

func adminOrder(query AdminUserQuery) func(...string) func(*entsql.Selector) {
	if query.Order == AdminOrderOldest {
		return ent.Asc
	}
	return ent.Desc
}

func whereAdminSearch(q *ent.UserQuery, search string) *ent.UserQuery {
	s := NormalizeAdminSearch(search)
	if s == "" {
		return q
	}
	return q.Where(AdminSearchPredicate(s))
}

func whereAdminState(q *ent.UserQuery, state string) *ent.UserQuery {
	pred, ok := AdminStatePredicate(state)
	if !ok {
		return q
	}
	return q.Where(pred)
}

func whereAdminLive(q *ent.UserQuery, query AdminUserQuery) *ent.UserQuery {
	if !query.OnlyLive {
		return q
	}
	return q.Where(user.IDIn(query.LiveIDs...))
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

type UserCounts struct {
	Total   int `json:"total"`
	Active  int `json:"active"`
	Paid    int `json:"paid"`
	Granted int `json:"granted"`
	Vip     int `json:"vip"`
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

func isGrantedCond(s *entsql.Selector) string {
	return statusIs(user.StatusPaid)(s) + " AND " + s.C(user.FieldSubscriptionSource) + " IN ('admin', 'giveaway')"
}

func (r *Users) UserStats(ctx context.Context) (UserCounts, error) {
	return r.stats.GetOrLoad(ctx, userStatsKey, r.loadUserStats)
}

func (r *Users) loadUserStats(ctx context.Context) (UserCounts, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (UserCounts, error) {
		var rows []UserCounts
		err := r.client.User.Query().
			Aggregate(
				ent.As(ent.Count(), "total"),
				ent.As(countIf(isActiveCond), "active"),
				ent.As(countIf(statusIs(user.StatusPaid)), "paid"),
				ent.As(countIf(isGrantedCond), "granted"),
				ent.As(countIf(statusIs(user.StatusVip)), "vip"),
			).
			Scan(ctx, &rows)
		if err != nil {
			return UserCounts{}, err
		}
		if len(rows) == 0 {
			return UserCounts{}, nil
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
