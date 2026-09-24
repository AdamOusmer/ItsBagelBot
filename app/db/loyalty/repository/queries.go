// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/app/db/loyalty/ent/counter"
	"ItsBagelBot/app/db/loyalty/ent/counterentry"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

const maxCounterName = 64

const (
	defaultTopLimit = 10
	maxTopLimit     = 100
)

var ErrInvalidInput = errors.New("invalid input")

func ValidCounterName(name string) (string, error) {
	n := normalizeName(name)
	if n == "" || len(n) > maxCounterName {
		return "", fmt.Errorf("%w: counter name", ErrInvalidInput)
	}
	if strings.Contains(n, ":") {
		return "", fmt.Errorf("%w: counter name", ErrInvalidInput)
	}
	return n, nil
}

func writableCounterName(userID uint64, name string) (string, error) {
	n, err := ValidCounterName(name)
	if err != nil {
		return "", err
	}
	if userID != 0 && data.SystemCounter(n) {
		return "", fmt.Errorf("%w: reserved counter name", ErrInvalidInput)
	}
	return n, nil
}

func ValidScope(scope string) (string, error) {
	switch scope {
	case "", data.CounterScopeChannel:
		return data.CounterScopeChannel, nil
	case data.CounterScopeBot, data.CounterScopeViewer, data.CounterScopeCommand, data.CounterScopeViewerCommand:
		return scope, nil
	default:
		return "", fmt.Errorf("%w: scope", ErrInvalidInput)
	}
}

func entryScoped(scope string) bool {
	switch scope {
	case data.CounterScopeViewer, data.CounterScopeCommand, data.CounterScopeViewerCommand:
		return true
	default:
		return false
	}
}

func bucketed(scope string) bool {
	return scope == data.CounterScopeCommand || scope == data.CounterScopeViewerCommand
}

func (r *Loyalty) BalanceGet(ctx context.Context, userID, viewerID uint64) (*ent.Balance, bool, error) {
	return getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerIDEQ(viewerID)).
			Only(ctx)
	})
}

func (r *Loyalty) Top(ctx context.Context, userID uint64, limit int) ([]*ent.Balance, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID)).
			Order(balance.ByPoints(entsql.OrderDesc()), balance.ByViewerID()).
			Limit(clampLimit(limit)).
			All(ctx)
	})
}

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

// The points >= amount guard must stay in the UPDATE's WHERE so concurrent spends cannot go negative.
func (r *Loyalty) BalanceSpend(ctx context.Context, userID uint64, viewerLogin string, amount int64) (*ent.Balance, bool, bool, error) {
	login, err := normalizeSpendTarget(viewerLogin, amount)
	if err != nil {
		return nil, false, false, err
	}
	row, found, err := getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerLoginEQ(login)).
			Order(balance.ByUpdatedAt(entsql.OrderDesc()), balance.ByViewerID()).
			First(ctx)
	})
	if err != nil || !found {
		return nil, false, found, err
	}
	n, err := r.client.Balance.Update().
		Where(balance.IDEQ(row.ID), balance.PointsGTE(amount)).
		AddPoints(-amount).
		Save(ctx)
	if err != nil {
		return nil, true, false, err
	}
	if n == 0 {
		fresh, ferr := r.client.Balance.Get(ctx, row.ID)
		if ferr != nil {
			return nil, true, false, ferr
		}
		return fresh, true, false, nil
	}
	spent, err := r.client.Balance.Get(ctx, row.ID)
	if err != nil {
		return nil, true, true, err
	}
	return spent, true, true, nil
}

func normalizeSpendTarget(viewerLogin string, amount int64) (string, error) {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(viewerLogin), "@"))
	if login == "" || amount <= 0 {
		return "", fmt.Errorf("%w: viewer_login/amount", ErrInvalidInput)
	}
	return login, nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultTopLimit
	}
	return min(limit, maxTopLimit)
}

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

func (r *Loyalty) viewerLogins(ctx context.Context, userID uint64, rows []*ent.CounterEntry) map[uint64]string {
	logins := map[uint64]string{}
	ids := legacyViewerIDs(rows)
	if len(ids) == 0 {
		return logins
	}
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

func legacyViewerIDs(rows []*ent.CounterEntry) []uint64 {
	seen := map[uint64]struct{}{}
	ids := make([]uint64, 0, len(rows))
	for _, e := range rows {
		if e.ViewerID == 0 || e.ViewerLogin != "" {
			continue
		}
		if _, dup := seen[e.ViewerID]; !dup {
			seen[e.ViewerID] = struct{}{}
			ids = append(ids, e.ViewerID)
		}
	}
	return ids
}

func entryTarget(row *ent.Counter, viewerID uint64, command string) (uint64, string, bool) {
	switch row.Scope {
	case data.CounterScopeCommand:
		cmd := normalizeCommand(command)
		return 0, cmd, cmd != ""
	case data.CounterScopeViewer:
		return viewerID, "", viewerID != 0
	case data.CounterScopeViewerCommand:
		return viewerID, normalizeCommand(command), viewerID != 0
	default:
		return 0, "", false
	}
}

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
	entryViewer, cmd, useEntry := entryTarget(row, viewerID, command)
	if !useEntry {
		return row, row.Value, true, nil
	}
	entry, entryFound, err := getOptional(ctx, func(ctx context.Context) (*ent.CounterEntry, error) {
		return r.client.CounterEntry.Query().
			Where(
				counterentry.UserIDEQ(userID),
				counterentry.NameEQ(n),
				counterentry.CommandEQ(cmd),
				counterentry.ViewerIDEQ(entryViewer),
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

func (r *Loyalty) CountersList(ctx context.Context, userID uint64) ([]*ent.Counter, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Counter, error) {
		q := r.client.Counter.Query().Where(counter.UserIDEQ(userID))
		if userID != 0 {
			q = q.Where(counter.NameNotIn(data.SystemCounterNames()...), counter.Not(counter.NameHasPrefix(data.TrialCounterPrefix)))
		}
		return q.Order(counter.ByName()).All(ctx)
	})
}

func (r *Loyalty) TrialCounters(ctx context.Context, userID uint64) ([]*ent.Counter, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Counter, error) {
		return r.client.Counter.Query().
			Where(counter.UserIDEQ(userID), counter.NameHasPrefix(data.TrialCounterPrefix)).
			Order(counter.ByName()).
			All(ctx)
	})
}

func (r *Loyalty) CounterBoard(ctx context.Context, name string, limit int) ([]*ent.Counter, error) {
	n, err := ValidCounterName(name)
	if err != nil {
		return nil, err
	}
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Counter, error) {
		return r.client.Counter.Query().
			Where(
				counter.NameEQ(n),
				counter.UserIDNEQ(0),
				counter.ScopeEQ(data.CounterScopeChannel),
			).
			Order(counter.ByValue(entsql.OrderDesc()), counter.ByUserID()).
			Limit(clampLimit(limit)).
			All(ctx)
	})
}

func (r *Loyalty) CounterCreate(ctx context.Context, userID uint64, name, scope string) (*ent.Counter, error) {
	n, err := writableCounterName(userID, name)
	if err != nil {
		return nil, err
	}
	s, err := ValidScope(scope)
	if err != nil {
		return nil, err
	}
	if (s == data.CounterScopeBot) != (userID == 0) {
		return nil, fmt.Errorf("%w: scope", ErrInvalidInput)
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

type SetTarget struct {
	ViewerID    uint64
	Command     string
	ViewerLogin string
}

func (r *Loyalty) CounterSet(ctx context.Context, userID uint64, name string, target SetTarget, value int64) (bool, error) {
	if value < 0 || value > data.MaxCounter {
		return false, fmt.Errorf("%w: counter value", ErrInvalidInput)
	}
	if _, err := writableCounterName(userID, name); err != nil {
		return false, err
	}
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
		entryViewer, cmd, targeted := entryTarget(row, target.ViewerID, target.Command)
		if !targeted {
			_, err := r.client.CounterEntry.Delete().
				Where(counterentry.UserIDEQ(userID), counterentry.NameEQ(row.Name)).
				Exec(ctx)
			return err
		}
		login := normalizeLogin(target.ViewerLogin)
		return r.client.CounterEntry.Create().
			SetUserID(userID).
			SetName(row.Name).
			SetCommand(cmd).
			SetViewerID(entryViewer).
			SetViewerLogin(login).
			SetValue(value).
			OnConflict(entsql.ConflictColumns(counterentry.FieldUserID, counterentry.FieldName, counterentry.FieldCommand, counterentry.FieldViewerID)).
			Update(func(u *ent.CounterEntryUpsert) {
				u.UpdateValue()
				if login != "" {
					u.UpdateViewerLogin()
				}
			}).
			Exec(ctx)
	})
}

// Must refuse an untargeted call, or it resets the whole counter.
func (r *Loyalty) CounterEntryDelete(ctx context.Context, userID uint64, name string, target SetTarget) (bool, error) {
	if _, err := writableCounterName(userID, name); err != nil {
		return false, err
	}
	row, _, found, err := r.CounterGet(ctx, userID, name, 0, "")
	if err != nil || !found {
		return found, err
	}
	if !entryScoped(row.Scope) {
		return false, nil
	}
	entryViewer, cmd, targeted := entryTarget(row, target.ViewerID, target.Command)
	if !targeted {
		return false, nil
	}
	return true, db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.CounterEntry.Delete().
			Where(
				counterentry.UserIDEQ(userID),
				counterentry.NameEQ(row.Name),
				counterentry.CommandEQ(cmd),
				counterentry.ViewerIDEQ(entryViewer),
			).
			Exec(ctx)
		return err
	})
}

func normalizeLogin(login string) string {
	l := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(login), "@"))
	if len(l) > maxCounterName {
		l = l[:maxCounterName]
	}
	return l
}

func (r *Loyalty) CounterRename(ctx context.Context, userID uint64, name, newName string) (bool, error) {
	n, err := writableCounterName(userID, name)
	if err != nil {
		return false, err
	}
	nn, err := writableCounterName(userID, newName)
	if err != nil || nn == n {
		return false, fmt.Errorf("%w: new name", ErrInvalidInput)
	}
	renamed := false
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, r.client, func(tx *ent.Tx) error {
			updated, err := tx.Counter.Update().
				Where(counter.UserIDEQ(userID), counter.NameEQ(n)).
				SetName(nn).
				Save(ctx)
			if err != nil {
				if ent.IsConstraintError(err) {
					return fmt.Errorf("%w: name taken", ErrInvalidInput)
				}
				return err
			}
			if updated == 0 {
				return nil
			}
			renamed = true
			return tx.CounterEntry.Update().
				Where(counterentry.UserIDEQ(userID), counterentry.NameEQ(n)).
				SetName(nn).
				Exec(ctx)
		})
	})
	return renamed, err
}

func withTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *Loyalty) CounterDelete(ctx context.Context, userID uint64, name string) error {
	n, err := writableCounterName(userID, name)
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
