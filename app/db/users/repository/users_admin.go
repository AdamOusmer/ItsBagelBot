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

// ErrUserNotFound is returned when a lookup by ID or username finds no row.
// Callers must use errors.Is; never compare the string directly.
var ErrUserNotFound = errors.New("user not found")

const (
	AdminUserPageSize     = 15
	AdminUserMaxPages     = 25
	AdminUserMaxSearchLen = 200
)

const (
	// userStatsKey is the single fixed key the stats tuple lives under: the
	// counts are global, so there is nothing to key them by.
	userStatsKey = "users:stats"

	// userStatsCapacity is 1 because of the line above. One key, one entry.
	userStatsCapacity int64 = 1

	// userStatsTTL is how stale the admin counts are allowed to be, and 60s is
	// a deliberate number rather than a default.
	//
	// What moves them: signups and tier changes, single digits per day. A count
	// that is a minute old is not a different answer to a human reading the
	// panel.
	//
	// Who reads them: discord-ingress presence polls counts.get every 5 minutes
	// from one replica, so it never observes an entry older than a fraction of
	// its own interval, and the admin console overview loads several panels at
	// once, which now collapse onto one query instead of one set of four each.
	//
	// Why there is a cache at all: the previous shape (four sequential COUNTs,
	// each taking the process DB gate) measured 3,001ms in production against
	// the counts RPC's 3s deadline. The single query fixes the cost; the TTL
	// keeps a burst of panel loads from paying it repeatedly.
	//
	// users-svc deliberately holds no Valkey client (see app/db/users/main.go),
	// so this stays in process. Every replica keeps its own copy; that is fine,
	// the value is advisory, not a ledger.
	userStatsTTL = 60 * time.Second
)

// FindUser looks up a user by numeric ID. Returns ErrUserNotFound when the
// row does not exist.
func (r *Users) FindUser(ctx context.Context, userID uint64) (*ent.User, error) {
	u, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.IDEQ(userID)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, ErrUserNotFound
	}
	return u, err
}

// FindUserByUsername looks up a user by exact username. Returns ErrUserNotFound
// when the row does not exist.
func (r *Users) FindUserByUsername(ctx context.Context, username string) (*ent.User, error) {
	u, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.UsernameEqualFold(username)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, ErrUserNotFound
	}
	return u, err
}

// AdminUserQuery bundles the admin list filters and pagination window so
// callers pass one value instead of a row of positional primitives.
type AdminUserQuery struct {
	// Search filters to rows whose username contains it (case-insensitive)
	// or whose numeric ID equals it exactly. Empty means no filter.
	Search string
	// State narrows to one effective user state (see AdminStatePredicate);
	// empty or unknown applies no filter.
	State string
	// Limit and Offset control pagination; the caller is responsible for
	// computing the correct fetch limit (pageSize+1 trick) before calling.
	Limit  int
	Offset int
}

// ListUsers returns rows ordered by most-recently-updated then by descending ID.
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

// AdminStatePredicate maps one effective user state to a predicate. Precedence
// mirrors the console's row color: banned beats inactive beats tier, so a
// banned VIP shows under "banned", not "vip".
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

// userStatsRow is the single-row result of the conditional-aggregate stats
// query. The column aliases below MUST match these tags: ent's scanner maps a
// result column onto a struct field by the `sql` tag and then the `json` tag,
// and a mismatch is not a compile error, it fails at runtime with
// "sql/scan: missing struct field for column".
type userStatsRow struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Paid   int `json:"paid"`
	Vip    int `json:"vip"`
}

// countIf builds COUNT(CASE WHEN <cond> THEN 1 END) for one aggregate column.
//
// CASE WHEN rather than MySQL's shorter COUNT(IF(cond, 1, NULL)): production
// runs MySQL (the driver is pinned to dialect.MySQL in pkg/db/provider.go) but
// this repository's tests run the very same query against an in-memory SQLite
// through enttest, and SQLite has no IF(). CASE WHEN is standard SQL and runs
// on both.
//
// COUNT rather than SUM: COUNT returns a BIGINT and is never NULL, so an empty
// table scans as 0 with no COALESCE wrapper, while SUM over no rows is NULL and
// would fail to scan into an int.
func countIf(cond func(*entsql.Selector) string) ent.AggregateFunc {
	return func(s *entsql.Selector) string {
		return "COUNT(CASE WHEN " + cond(s) + " THEN 1 END)"
	}
}

// statusIs builds the "status equals this tier" condition for countIf.
//
// The value is concatenated into the SQL text rather than bound as a parameter
// because an ent.AggregateFunc returns a raw string and has nowhere to put an
// argument. That is safe here and only here: user.Status is a generated enum
// constant (free, paid, vip), never a caller-supplied string. Do not widen this
// helper to take a plain string.
func statusIs(status user.Status) func(*entsql.Selector) string {
	return func(s *entsql.Selector) string {
		return s.C(user.FieldStatus) + " = '" + string(status) + "'"
	}
}

// isActiveCond matches the previous active count exactly: is_active true,
// whether or not the row is banned. The bare column is the condition; both
// MySQL and SQLite store the boolean as 0/1 and treat non-zero as true.
func isActiveCond(s *entsql.Selector) string {
	return s.C(user.FieldIsActive)
}

// UserStats returns the four counts the admin stats panel displays: total
// users, active users, paid users, and VIP users.
//
// This used to be four sequential Count() queries, each separately acquiring
// the process-wide DB gate. In production one call measured 3,001ms against the
// counts RPC's 3s deadline, so the caller got a timeout instead of a number. It
// is now one conditional-aggregate query behind a short process cache.
func (r *Users) UserStats(ctx context.Context) (total, active, paid, vip int, err error) {
	row, err := r.stats.GetOrLoad(ctx, userStatsKey, r.loadUserStats)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return row.Total, row.Active, row.Paid, row.Vip, nil
}

// loadUserStats runs the one aggregate query behind UserStats. An aggregate
// with no GROUP BY always yields exactly one row; the length check only keeps a
// driver surprise from becoming an index panic.
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

// EnrollmentDay is one UTC day's signup count.
type EnrollmentDay struct {
	Date  string // YYYY-MM-DD
	Count int
}

// EnrollmentSeries buckets user signups per UTC day over the trailing `days`
// window, today included. Every day in the window is present, zero-filled, so
// callers can chart the series without gap handling.
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

// HasToken reports whether the user currently has a stored token of the given
// type and platform.
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

// ClearToken deletes the token row matching the given type and platform for the
// user. It is a no-op when no matching row exists.
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

// ResetTokens deletes all token rows for the user. Used by the admin reset
// operation to wipe a user's stored credentials.
func (r *Users) ResetTokens(ctx context.Context, userID uint64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Tokens.Delete().
			Where(tokens.HasUserWith(user.IDEQ(userID))).
			Exec(ctx)
		return err
	})
}

// NormalizeAdminSearch trims whitespace and truncates to AdminUserMaxSearchLen.
func NormalizeAdminSearch(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > AdminUserMaxSearchLen {
		return string(runes[:AdminUserMaxSearchLen])
	}
	return s
}

// AdminSearchPredicate builds an OR predicate matching rows whose username
// contains the search string (case-insensitive) or whose numeric ID equals it.
func AdminSearchPredicate(search string) predicate.User {
	preds := []predicate.User{
		user.UsernameContainsFold(search),
	}
	if id, err := strconv.ParseUint(search, 10, 64); err == nil {
		preds = append(preds, user.IDEQ(id))
	}
	return user.Or(preds...)
}
