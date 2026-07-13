package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/predicate"
	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/pkg/db"
)

// ErrUserNotFound is returned when a lookup by ID or username finds no row.
// Callers must use errors.Is; never compare the string directly.
var ErrUserNotFound = errors.New("user not found")

const (
	AdminUserPageSize     = 15
	AdminUserMaxPages     = 25
	AdminUserMaxSearchLen = 200
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

// ListUsers returns rows ordered by most-recently-updated then by descending
// ID. When search is non-empty the results are filtered to rows whose username
// contains the search string (case-insensitive) or whose numeric ID equals it
// exactly. limit and offset control pagination; the caller is responsible for
// computing the correct fetchLimit (pageSize+1 trick) before calling.
func (r *Users) ListUsers(ctx context.Context, search string, limit, offset int) ([]*ent.User, error) {
	q := r.client.User.Query().Order(ent.Desc(user.FieldUpdatedAt), ent.Desc(user.FieldID))
	if s := NormalizeAdminSearch(search); s != "" {
		q = q.Where(AdminSearchPredicate(s))
	}
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return q.Offset(offset).Limit(limit).All(ctx)
	})
}

// UserStats returns the four counts the admin stats panel displays: total
// users, active users, paid users, and VIP users.
func (r *Users) UserStats(ctx context.Context) (total, active, paid, vip int, err error) {
	total, err = db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.User.Query().Count(ctx)
	})
	if err != nil {
		return
	}
	active, err = db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.User.Query().Where(user.IsActiveEQ(true)).Count(ctx)
	})
	if err != nil {
		return
	}
	paid, err = db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.User.Query().Where(user.StatusEQ(user.StatusPaid)).Count(ctx)
	})
	if err != nil {
		return
	}
	vip, err = db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.User.Query().Where(user.StatusEQ(user.StatusVip)).Count(ctx)
	})
	return
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
