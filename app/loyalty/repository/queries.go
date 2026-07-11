package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/app/loyalty/ent"
	"ItsBagelBot/app/loyalty/ent/balance"
	"ItsBagelBot/app/loyalty/ent/counter"
	"ItsBagelBot/app/loyalty/ent/counterentry"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

// maxCounterName mirrors the schema's MaxLen; enforced at the trust boundary
// so a hostile RPC can never hit the column constraint as a DB error.
const maxCounterName = 64

// defaultTopLimit bounds a leaderboard read when the caller sent no (or a
// silly) limit.
const (
	defaultTopLimit = 10
	maxTopLimit     = 100
)

// ErrInvalidInput marks trust-boundary rejections; the RPC layer maps it to a
// "bad request" reply instead of a generic failure.
var ErrInvalidInput = errors.New("invalid input")

// ValidCounterName reports the normalized name, or an error when it is empty
// or oversized.
func ValidCounterName(name string) (string, error) {
	n := normalizeName(name)
	if n == "" || len(n) > maxCounterName {
		return "", fmt.Errorf("%w: counter name", ErrInvalidInput)
	}
	return n, nil
}

// ValidScope reports the canonical scope, defaulting empty to channel.
func ValidScope(scope string) (string, error) {
	switch scope {
	case "", data.CounterScopeChannel:
		return data.CounterScopeChannel, nil
	case data.CounterScopeViewer, data.CounterScopeViewerCommand:
		return scope, nil
	default:
		return "", fmt.Errorf("%w: scope", ErrInvalidInput)
	}
}

// entryScoped reports whether a scope keeps its values in counter_entries.
func entryScoped(scope string) bool {
	return scope == data.CounterScopeViewer || scope == data.CounterScopeViewerCommand
}

// BalanceGet returns one viewer's standing. A missing row is (zero, false, nil).
func (r *Loyalty) BalanceGet(ctx context.Context, userID, viewerID uint64) (*ent.Balance, bool, error) {
	return getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerIDEQ(viewerID)).
			Only(ctx)
	})
}

// Top returns the channel's top standings by points.
func (r *Loyalty) Top(ctx context.Context, userID uint64, limit int) ([]*ent.Balance, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID)).
			Order(balance.ByPoints(entsql.OrderDesc()), balance.ByViewerID()).
			Limit(clampLimit(limit)).
			All(ctx)
	})
}

// BalanceAdjust writes a viewer's points by login (a mod's "!points set/add
// @user" — chat knows the target's login, not their id). absolute sets the
// value; otherwise value is a delta. The row must already exist (any accrual
// creates it); an unseen login is (nil, false, nil) so the caller can answer
// "haven't seen them yet" instead of inventing an id-less row. Renames can
// leave several rows carrying one old login; the freshest wins.
func (r *Loyalty) BalanceAdjust(ctx context.Context, userID uint64, viewerLogin string, value int64, absolute bool) (*ent.Balance, bool, error) {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(viewerLogin), "@"))
	if login == "" {
		return nil, false, fmt.Errorf("%w: viewer_login", ErrInvalidInput)
	}
	row, found, err := getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerLoginEQ(login)).
			Order(balance.ByUpdatedAt(entsql.OrderDesc()), balance.ByViewerID()).
			First(ctx)
	})
	if err != nil || !found {
		return nil, found, err
	}
	return row, true, db.WithExec(ctx, func(ctx context.Context) error {
		upd := r.client.Balance.UpdateOneID(row.ID)
		if absolute {
			row.Points = value
			upd.SetPoints(value)
		} else {
			row.Points += value
			upd.AddPoints(value)
		}
		return upd.Exec(ctx)
	})
}

// clampLimit bounds a caller-provided page size, defaulting a missing one.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultTopLimit
	}
	return min(limit, maxTopLimit)
}

// CounterEntries lists an entry-scoped counter's stored buckets, highest
// value first, with each viewer's login resolved from their balance row (the
// dashboard's per-counter leaderboard).
func (r *Loyalty) CounterEntries(ctx context.Context, userID uint64, name string, limit int) ([]*ent.CounterEntry, map[uint64]string, error) {
	n, err := ValidCounterName(name)
	if err != nil {
		return nil, nil, err
	}
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.CounterEntry, error) {
		return r.client.CounterEntry.Query().
			Where(counterentry.UserIDEQ(userID), counterentry.NameEQ(n)).
			Order(counterentry.ByValue(entsql.OrderDesc()), counterentry.ByViewerID()).
			Limit(clampLimit(limit)).
			All(ctx)
	})
	if err != nil || len(rows) == 0 {
		return rows, nil, err
	}
	return rows, r.viewerLogins(ctx, userID, rows), nil
}

// viewerLogins resolves the display login for each distinct viewer in rows
// from their balance row. Best-effort: logins are cosmetic, the entries
// themselves are the answer, so a read failure returns an empty map.
func (r *Loyalty) viewerLogins(ctx context.Context, userID uint64, rows []*ent.CounterEntry) map[uint64]string {
	seen := map[uint64]struct{}{}
	ids := make([]uint64, 0, len(rows))
	for _, e := range rows {
		if _, dup := seen[e.ViewerID]; !dup {
			seen[e.ViewerID] = struct{}{}
			ids = append(ids, e.ViewerID)
		}
	}

	logins := map[uint64]string{}
	bals, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerIDIn(ids...)).
			All(ctx)
	})
	if err != nil {
		return logins
	}
	for _, b := range bals {
		if b.ViewerLogin != "" {
			logins[b.ViewerID] = b.ViewerLogin
		}
	}
	return logins
}

// CounterGet resolves one counter: the definition, plus the effective value —
// the row's own value for channel scope; the given viewer's entry (0 when the
// viewer has none) for the entry scopes, with command selecting the bucket of
// a viewer+command counter; and the row value when no viewer was given.
func (r *Loyalty) CounterGet(ctx context.Context, userID uint64, name string, viewerID uint64, command string) (*ent.Counter, int64, bool, error) {
	n, err := ValidCounterName(name)
	if err != nil {
		return nil, 0, false, err
	}
	row, found, err := getOptional(ctx, func(ctx context.Context) (*ent.Counter, error) {
		return r.client.Counter.Query().
			Where(counter.UserIDEQ(userID), counter.NameEQ(n)).
			Only(ctx)
	})
	if err != nil || !found {
		return nil, 0, found, err
	}
	if !entryScoped(row.Scope) || viewerID == 0 {
		return row, row.Value, true, nil
	}
	cmd := ""
	if row.Scope == data.CounterScopeViewerCommand {
		cmd = normalizeCommand(command)
	}
	entry, entryFound, err := getOptional(ctx, func(ctx context.Context) (*ent.CounterEntry, error) {
		return r.client.CounterEntry.Query().
			Where(
				counterentry.UserIDEQ(userID),
				counterentry.NameEQ(n),
				counterentry.CommandEQ(cmd),
				counterentry.ViewerIDEQ(viewerID),
			).
			Only(ctx)
	})
	if err != nil {
		return nil, 0, false, err
	}
	if !entryFound {
		return row, 0, true, nil
	}
	return row, entry.Value, true, nil
}

// CountersList returns the channel's counter definitions.
func (r *Loyalty) CountersList(ctx context.Context, userID uint64) ([]*ent.Counter, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Counter, error) {
		return r.client.Counter.Query().
			Where(counter.UserIDEQ(userID)).
			Order(counter.ByName()).
			All(ctx)
	})
}

// CounterCreate upserts a counter definition. An existing counter keeps its
// value and scope (create is idempotent, not a reset).
func (r *Loyalty) CounterCreate(ctx context.Context, userID uint64, name, scope string) (*ent.Counter, error) {
	n, err := ValidCounterName(name)
	if err != nil {
		return nil, err
	}
	s, err := ValidScope(scope)
	if err != nil {
		return nil, err
	}
	return db.WithQuery(ctx, func(ctx context.Context) (*ent.Counter, error) {
		err := r.client.Counter.Create().
			SetUserID(userID).
			SetName(n).
			SetScope(s).
			OnConflict(entsql.ConflictColumns(counter.FieldUserID, counter.FieldName)).
			Ignore().
			Exec(ctx)
		if err != nil {
			return nil, err
		}
		return r.client.Counter.Query().
			Where(counter.UserIDEQ(userID), counter.NameEQ(n)).
			Only(ctx)
	})
}

// CounterSet writes an absolute value. Channel scope sets the row value. For
// the entry scopes, a non-zero viewerID upserts that viewer's entry (command
// selects the bucket of a viewer+command counter); a zero viewerID resets the
// whole counter (deletes every entry) — the "!counter reset" semantics. A
// missing counter is (false, nil).
func (r *Loyalty) CounterSet(ctx context.Context, userID uint64, name string, viewerID uint64, command string, value int64) (bool, error) {
	row, _, found, err := r.CounterGet(ctx, userID, name, 0, "")
	if err != nil || !found {
		return found, err
	}
	return true, db.WithExec(ctx, func(ctx context.Context) error {
		if !entryScoped(row.Scope) {
			return r.client.Counter.Update().
				Where(counter.UserIDEQ(userID), counter.NameEQ(row.Name)).
				SetValue(value).
				Exec(ctx)
		}
		if viewerID == 0 {
			_, err := r.client.CounterEntry.Delete().
				Where(counterentry.UserIDEQ(userID), counterentry.NameEQ(row.Name)).
				Exec(ctx)
			return err
		}
		cmd := ""
		if row.Scope == data.CounterScopeViewerCommand {
			cmd = normalizeCommand(command)
		}
		return r.client.CounterEntry.Create().
			SetUserID(userID).
			SetName(row.Name).
			SetCommand(cmd).
			SetViewerID(viewerID).
			SetValue(value).
			OnConflict(entsql.ConflictColumns(counterentry.FieldUserID, counterentry.FieldName, counterentry.FieldCommand, counterentry.FieldViewerID)).
			UpdateValue().
			Exec(ctx)
	})
}

// CounterDelete removes a counter and its viewer entries.
func (r *Loyalty) CounterDelete(ctx context.Context, userID uint64, name string) error {
	n, err := ValidCounterName(name)
	if err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		if _, err := r.client.CounterEntry.Delete().
			Where(counterentry.UserIDEQ(userID), counterentry.NameEQ(n)).
			Exec(ctx); err != nil {
			return err
		}
		_, err := r.client.Counter.Delete().
			Where(counter.UserIDEQ(userID), counter.NameEQ(n)).
			Exec(ctx)
		return err
	})
}

// DeleteAllForUser removes every loyalty row of a deleted broadcaster account.
func (r *Loyalty) DeleteAllForUser(ctx context.Context, userID uint64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		if _, err := r.client.Balance.Delete().Where(balance.UserIDEQ(userID)).Exec(ctx); err != nil {
			return err
		}
		if _, err := r.client.CounterEntry.Delete().Where(counterentry.UserIDEQ(userID)).Exec(ctx); err != nil {
			return err
		}
		_, err := r.client.Counter.Delete().Where(counter.UserIDEQ(userID)).Exec(ctx)
		return err
	})
}

// getOptional runs one Only-style query through the DB gate and maps ent's
// not-found to (zero, false, nil).
func getOptional[T any](ctx context.Context, fn func(context.Context) (*T, error)) (*T, bool, error) {
	row, err := db.WithQuery(ctx, fn)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return row, true, nil
}
