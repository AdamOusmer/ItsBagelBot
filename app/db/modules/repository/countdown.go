// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/countdown"
	"ItsBagelBot/app/db/modules/ent/countdowndelivery"
	"ItsBagelBot/app/db/modules/ent/countdownoperation"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/db"
)

const CountdownLease = time.Minute
const maxCountdownNames = 128

type CountdownResult struct {
	Found, Capped bool
	ExpiresAtMS   int64
}
type CountdownDelivery struct {
	UserID                  uint64
	Name, Generation, Token string
	ExpiresAtMS             int64
	Progress                map[string]string
}

func countdownName(userID uint64, raw string) (string, error) {
	name, ok := automation.VariableName(raw)
	if userID == 0 || !ok {
		return "", fmt.Errorf("%w: countdown identity", ErrInvalidInput)
	}
	return name, nil
}

// CountdownWrite serializes names and deduplicates effects per channel and operation ID.
func (r *RecipeState) CountdownWrite(ctx context.Context, userID uint64, raw, verb string, seconds int64, operationID string) (out CountdownResult, err error) {
	name, err := countdownName(userID, raw)
	if err != nil {
		return out, err
	}
	if (verb != "start" && verb != "add") || seconds <= 0 || seconds > int64(automation.MaxCooldownSeconds) || len(operationID) == 0 || len(operationID) > 256 {
		return out, fmt.Errorf("%w: countdown operation", ErrInvalidInput)
	}
	err = db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
				// This existing per-channel mutex serializes the limit and operation ledger.
				if err := lockVariableNames(ctx, tx, VariableOwner{UserID: userID, Scope: automation.VariableScopeChannel}); err != nil {
					return err
				}
				prev, e := tx.CountdownOperation.Query().Where(countdownoperation.UserID(userID), countdownoperation.OperationID(operationID)).Only(ctx)
				if e == nil {
					if prev.Name != name || prev.Verb != verb || prev.Seconds != seconds {
						return fmt.Errorf("%w: operation ID reused", ErrInvalidInput)
					}
					out = CountdownResult{Found: prev.Found, Capped: prev.Capped, ExpiresAtMS: prev.ExpiresAtMs}
					return nil
				}
				if !ent.IsNotFound(e) {
					return e
				}
				row, e := tx.Countdown.Query().Where(countdown.UserID(userID), countdown.Name(name)).Only(ctx)
				if e != nil && !ent.IsNotFound(e) {
					return e
				}
				now := time.Now().UnixMilli()
				if verb == "start" {
					deadline := now + seconds*1000
					if row == nil {
						count, e := tx.Countdown.Query().Where(countdown.UserID(userID)).Count(ctx)
						if e != nil {
							return e
						}
						if count >= maxCountdownNames {
							return ErrVariableLimit
						}
						e = tx.Countdown.Create().SetUserID(userID).SetName(name).SetExpiresAtMs(deadline).SetGeneration(rand.Text()).Exec(ctx)
						if e != nil {
							return e
						}
					} else if e = tx.Countdown.UpdateOneID(row.ID).SetExpiresAtMs(deadline).SetGeneration(rand.Text()).Exec(ctx); e != nil {
						return e
					}
					out = CountdownResult{Found: true, ExpiresAtMS: deadline}
				} else if row != nil && row.ExpiresAtMs > now {
					deadline := row.ExpiresAtMs + seconds*1000
					ceiling := now + int64(automation.MaxCooldownSeconds)*1000
					capped := deadline > ceiling
					if capped {
						deadline = ceiling
					}
					if e = tx.Countdown.UpdateOneID(row.ID).SetExpiresAtMs(deadline).Exec(ctx); e != nil {
						return e
					}
					out = CountdownResult{Found: true, Capped: capped, ExpiresAtMS: deadline}
				}
				return tx.CountdownOperation.Create().SetUserID(userID).SetOperationID(operationID).SetName(name).SetVerb(verb).SetSeconds(seconds).SetFound(out.Found).SetCapped(out.Capped).SetExpiresAtMs(out.ExpiresAtMS).SetCreatedAtMs(now).Exec(ctx)
			})
		})
	})
	return out, err
}
func (r *RecipeState) CountdownPeek(ctx context.Context, userID uint64, raw string) (CountdownResult, error) {
	name, err := countdownName(userID, raw)
	if err != nil {
		return CountdownResult{}, err
	}
	row, err := db.WithRead(ctx, func(ctx context.Context) (*ent.Countdown, error) {
		return r.client.Countdown.Query().Where(countdown.UserID(userID), countdown.Name(name), countdown.ExpiresAtMsGT(time.Now().UnixMilli())).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return CountdownResult{}, nil
	}
	if err != nil {
		return CountdownResult{}, err
	}
	return CountdownResult{Found: true, ExpiresAtMS: row.ExpiresAtMs}, nil
}

// Claim promotes expired countdowns into immutable deliveries, then leases one.
func (r *RecipeState) CountdownClaim(ctx context.Context) (CountdownDelivery, bool, error) {
	now := time.Now().UnixMilli()
	if err := r.pruneCountdownOperations(ctx, now); err != nil {
		return CountdownDelivery{}, false, err
	}
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.Countdown, error) {
		return r.client.Countdown.Query().Where(countdown.ExpiresAtMsLTE(now)).Order(ent.Asc(countdown.FieldExpiresAtMs)).Limit(32).All(ctx)
	})
	if err != nil {
		return CountdownDelivery{}, false, err
	}
	for _, row := range rows {
		err = db.RetryTx(ctx, func(ctx context.Context) error {
			return db.WithExec(ctx, func(ctx context.Context) error {
				return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
					if err := lockVariableNames(ctx, tx, VariableOwner{UserID: row.UserID, Scope: automation.VariableScopeChannel}); err != nil {
						return err
					}
					n, err := tx.Countdown.Delete().Where(countdown.ID(row.ID), countdown.Generation(row.Generation), countdown.ExpiresAtMs(row.ExpiresAtMs)).Exec(ctx)
					if err != nil || n == 0 {
						return err
					}
					return tx.CountdownDelivery.Create().SetUserID(row.UserID).SetName(row.Name).SetGeneration(row.Generation).SetExpiresAtMs(row.ExpiresAtMs).SetProgress(map[string]string{}).Exec(ctx)
				})
			})
		})
		if err != nil {
			return CountdownDelivery{}, false, err
		}
	}
	now = time.Now().UnixMilli()
	pending, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.CountdownDelivery, error) {
		return r.client.CountdownDelivery.Query().Where(countdowndelivery.LeaseUntilMsLTE(now)).Order(ent.Asc(countdowndelivery.FieldExpiresAtMs)).Limit(32).All(ctx)
	})
	if err != nil {
		return CountdownDelivery{}, false, err
	}
	for _, row := range pending {
		token := rand.Text()
		leaseNow := time.Now().UnixMilli()
		var n int
		e := db.WithExec(ctx, func(ctx context.Context) error {
			var err error
			n, err = r.client.CountdownDelivery.Update().Where(countdowndelivery.ID(row.ID), countdowndelivery.LeaseUntilMsLTE(now)).SetLeaseToken(token).SetLeaseUntilMs(leaseNow + CountdownLease.Milliseconds()).Save(ctx)
			return err
		})
		if e != nil {
			return CountdownDelivery{}, false, e
		}
		if n == 1 {
			return CountdownDelivery{UserID: row.UserID, Name: row.Name, Generation: row.Generation, Token: token, ExpiresAtMS: row.ExpiresAtMs, Progress: row.Progress}, true, nil
		}
	}
	return CountdownDelivery{}, false, nil
}

// Ack is fenced by generation, token, deadline and the unexpired lease.
func (r *RecipeState) CountdownAck(ctx context.Context, d CountdownDelivery) (bool, error) {
	name, err := countdownName(d.UserID, d.Name)
	if err != nil {
		return false, err
	}
	if d.Generation == "" || d.Token == "" || d.ExpiresAtMS <= 0 {
		return false, errors.Join(ErrInvalidInput, errors.New("countdown claim"))
	}
	var n int
	err = db.WithExec(ctx, func(ctx context.Context) error {
		var e error
		n, e = r.client.CountdownDelivery.Delete().Where(countdowndelivery.UserID(d.UserID), countdowndelivery.Name(name), countdowndelivery.Generation(d.Generation), countdowndelivery.ExpiresAtMs(d.ExpiresAtMS), countdowndelivery.LeaseToken(d.Token), countdowndelivery.LeaseUntilMsGT(time.Now().UnixMilli())).Exec(ctx)
		return e
	})
	return n == 1, err
}

// Retry identities are retained for 31 days, covering the maximum 30-day countdown window and typical event redelivery. Each claim removes at most one bounded batch.
func (r *RecipeState) pruneCountdownOperations(ctx context.Context, now int64) error {
	query := "DELETE FROM countdown_operations WHERE id IN (SELECT id FROM countdown_operations WHERE created_at_ms<? ORDER BY created_at_ms LIMIT ?)"
	if r.dialect != "sqlite3" {
		query = "DELETE FROM countdown_operations WHERE created_at_ms<? ORDER BY created_at_ms LIMIT ?"
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.sqldb.ExecContext(ctx, query, now-int64(31*24*time.Hour/time.Millisecond), 1000)
		return err
	})
}

// CountdownProgress records a monotonic checkpoint only while the delivery lease is held.
func (r *RecipeState) CountdownProgress(ctx context.Context, d CountdownDelivery, key, status string) (found bool, err error) {
	name, err := countdownName(d.UserID, d.Name)
	if err != nil {
		return false, err
	}
	if d.Generation == "" || d.Token == "" || d.ExpiresAtMS <= 0 || len(key) == 0 || len(key) > 256 || (status != "conditions_passed" && status != "completed") {
		return false, fmt.Errorf("%w: countdown progress", ErrInvalidInput)
	}
	err = db.RetryTx(ctx, func(ctx context.Context) error {
		found = false
		return db.WithExec(ctx, func(ctx context.Context) error {
			return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
				query := tx.CountdownDelivery.Query().Where(countdowndelivery.UserID(d.UserID), countdowndelivery.Name(name), countdowndelivery.Generation(d.Generation), countdowndelivery.ExpiresAtMs(d.ExpiresAtMS), countdowndelivery.LeaseToken(d.Token), countdowndelivery.LeaseUntilMsGT(time.Now().UnixMilli()))
				if r.dialect != "sqlite3" {
					query.ForUpdate()
				}
				row, e := query.Only(ctx)
				if ent.IsNotFound(e) {
					return nil
				}
				if e != nil {
					return e
				}
				progress := row.Progress
				if progress == nil {
					progress = make(map[string]string)
				}
				if _, exists := progress[key]; !exists && len(progress) >= 1024 {
					return ErrVariableLimit
				}
				if progress[key] != "completed" {
					progress[key] = status
				}
				n, e := tx.CountdownDelivery.Update().Where(countdowndelivery.ID(row.ID), countdowndelivery.LeaseToken(d.Token), countdowndelivery.LeaseUntilMsGT(time.Now().UnixMilli())).SetProgress(progress).Save(ctx)
				found = n == 1
				return e
			})
		})
	})
	return found, err
}
