// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/cooldownclaim"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

const (
	maxCooldownWindows   = 2
	maxCooldownKeyBytes  = 128
	maxCooldownTTL       = automation.MaxCooldownSeconds * time.Second
	cooldownPruneBatch   = 1000
	cooldownReleaseGrace = 5 * time.Second
)

// ErrInvalidInput marks a request no retry can fix.
var ErrInvalidInput = errors.New("invalid input")

// RecipeState owns recipe variables and cooldown windows over one day.
type RecipeState struct {
	client  *ent.Client
	sqldb   *sql.DB
	dialect string
}

func NewRecipeState(client *ent.Client, driver *entsql.Driver) *RecipeState {
	return &RecipeState{client: client, sqldb: driver.DB(), dialect: driver.Dialect()}
}

type CooldownWindow struct {
	Key string
	TTL time.Duration
}

func (w CooldownWindow) valid() error {
	if w.Key == "" || len(w.Key) > maxCooldownKeyBytes {
		return fmt.Errorf("%w: cooldown key", ErrInvalidInput)
	}
	if w.TTL <= 0 || w.TTL > maxCooldownTTL {
		return fmt.Errorf("%w: cooldown window", ErrInvalidInput)
	}
	return nil
}

type CooldownClaim struct {
	UserID  uint64
	Windows []CooldownWindow
}

func (c CooldownClaim) valid() error {
	if c.UserID == 0 {
		return fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	if len(c.Windows) == 0 || len(c.Windows) > maxCooldownWindows {
		return fmt.Errorf("%w: cooldown windows", ErrInvalidInput)
	}
	for _, window := range c.Windows {
		if err := window.valid(); err != nil {
			return err
		}
	}
	return nil
}

// CooldownHold names one window; its expiry tells this claim apart from any later one on the same key.
type CooldownHold struct {
	Key         string
	ExpiresAtMS int64
}

// CooldownClaimAll wins every window or none, claiming in the given order. A refusal returns the window that refused, unless its row is already gone.
func (r *RecipeState) CooldownClaimAll(ctx context.Context, claim CooldownClaim) ([]CooldownHold, bool, error) {
	if err := claim.valid(); err != nil {
		return nil, false, err
	}
	holds := make([]CooldownHold, 0, len(claim.Windows))
	for _, window := range claim.Windows {
		hold, won, err := r.claimCooldownWindow(ctx, claim.UserID, window)
		if err != nil {
			return nil, false, errors.Join(err, r.releaseAfterRefusal(ctx, claim.UserID, holds))
		}
		if !won {
			return r.refusedBy(ctx, claim.UserID, window.Key), false, r.releaseAfterRefusal(ctx, claim.UserID, holds)
		}
		holds = append(holds, hold)
	}
	return holds, true, nil
}

// The refusal already stands, so a failed read only costs the caller its marker rewrite.
func (r *RecipeState) refusedBy(ctx context.Context, userID uint64, key string) []CooldownHold {
	row, err := db.WithRead(ctx, func(ctx context.Context) (*ent.CooldownClaim, error) {
		return r.client.CooldownClaim.Query().Where(cooldownclaim.UserID(userID), cooldownclaim.CooldownKey(key)).Only(ctx)
	})
	if err != nil {
		return nil
	}
	return []CooldownHold{{Key: key, ExpiresAtMS: row.ExpiresAtMs}}
}

func (r *RecipeState) releaseAfterRefusal(ctx context.Context, userID uint64, holds []CooldownHold) error {
	if len(holds) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cooldownReleaseGrace)
	defer cancel()
	return r.CooldownRelease(ctx, userID, holds)
}

func (r *RecipeState) claimCooldownWindow(ctx context.Context, userID uint64, window CooldownWindow) (CooldownHold, bool, error) {
	now := time.Now()
	hold := CooldownHold{Key: window.Key, ExpiresAtMS: now.Add(window.TTL).UnixMilli()}
	won := false
	err := db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			var err error
			won, err = r.takeCooldownRow(ctx, userID, hold, now.UnixMilli())
			return err
		})
	})
	return hold, won, err
}

// Two autocommit statements, never one transaction: a failed insert keeps a shared lock on the row until commit, and two of those deadlock on the next write.
func (r *RecipeState) takeCooldownRow(ctx context.Context, userID uint64, hold CooldownHold, nowMS int64) (bool, error) {
	revived, err := r.client.CooldownClaim.Update().
		Where(cooldownclaim.UserID(userID), cooldownclaim.CooldownKey(hold.Key), cooldownclaim.ExpiresAtMsLTE(nowMS)).
		SetExpiresAtMs(hold.ExpiresAtMS).
		Save(ctx)
	if err != nil || revived > 0 {
		return revived > 0, err
	}
	err = r.client.CooldownClaim.Create().SetUserID(userID).SetCooldownKey(hold.Key).SetExpiresAtMs(hold.ExpiresAtMS).Exec(ctx)
	if ent.IsConstraintError(err) {
		return false, nil
	}
	return err == nil, err
}

// CooldownRelease gives back only the claims it names, so it cannot erase a later winner.
func (r *RecipeState) CooldownRelease(ctx context.Context, userID uint64, holds []CooldownHold) error {
	if userID == 0 {
		return fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	var failed error
	for _, hold := range holds {
		failed = errors.Join(failed, r.releaseCooldownRow(ctx, userID, hold))
	}
	return failed
}

func (r *RecipeState) releaseCooldownRow(ctx context.Context, userID uint64, hold CooldownHold) error {
	_, err := r.deleteCooldownRow(ctx, CooldownRow{UserID: userID, CooldownHold: hold})
	return err
}

func (r *RecipeState) deleteCooldownRow(ctx context.Context, row CooldownRow) (int, error) {
	deleted := 0
	err := db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			var err error
			deleted, err = r.client.CooldownClaim.Delete().
				Where(cooldownclaim.UserID(row.UserID), cooldownclaim.CooldownKey(row.Key), cooldownclaim.ExpiresAtMs(row.ExpiresAtMS)).
				Exec(ctx)
			return err
		})
	})
	return deleted, err
}

// CooldownPeek reads which of the named windows still run; it takes nothing.
func (r *RecipeState) CooldownPeek(ctx context.Context, userID uint64, wanted []CooldownHold) ([]CooldownHold, error) {
	if userID == 0 {
		return nil, fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	if len(wanted) == 0 || len(wanted) > maxCooldownWindows {
		return nil, fmt.Errorf("%w: cooldown windows", ErrInvalidInput)
	}
	keys := make([]string, len(wanted))
	for i, hold := range wanted {
		keys[i] = hold.Key
	}
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.CooldownClaim, error) {
		return r.client.CooldownClaim.Query().
			Where(cooldownclaim.UserID(userID), cooldownclaim.CooldownKeyIn(keys...), cooldownclaim.ExpiresAtMsGT(time.Now().UnixMilli())).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	holds := make([]CooldownHold, len(rows))
	for i, row := range rows {
		holds[i] = CooldownHold{Key: row.CooldownKey, ExpiresAtMS: row.ExpiresAtMs}
	}
	return holds, nil
}

// PruneExpiredCooldowns walks the expiry index in bounded batches; it only removes rows a claim would overwrite anyway.
func (r *RecipeState) PruneExpiredCooldowns(ctx context.Context) error {
	now := time.Now().UnixMilli()
	for ctx.Err() == nil {
		deleted, err := r.pruneCooldownBatch(ctx, now)
		if err != nil || deleted < cooldownPruneBatch {
			return err
		}
	}
	return ctx.Err()
}

func (r *RecipeState) pruneCooldownBatch(ctx context.Context, nowMS int64) (int64, error) {
	result, err := r.sqldb.ExecContext(ctx, r.cooldownPruneQuery(), nowMS, cooldownPruneBatch)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *RecipeState) cooldownPruneQuery() string {
	expired := " WHERE expires_at_ms<=? ORDER BY expires_at_ms LIMIT ?"
	if r.dialect == "sqlite3" {
		return "DELETE FROM cooldown_claims WHERE id IN (SELECT id FROM cooldown_claims" + expired + ")"
	}
	return "DELETE FROM cooldown_claims" + expired
}

// CooldownRow is one stored window due to leave the database.
type CooldownRow struct {
	UserID uint64
	CooldownHold
}

func (r *RecipeState) dueCooldowns(ctx context.Context, now time.Time, limit int) ([]CooldownRow, error) {
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.CooldownClaim, error) {
		return r.client.CooldownClaim.Query().
			Where(cooldownclaim.ExpiresAtMsGT(now.UnixMilli()), cooldownclaim.ExpiresAtMsLTE(now.Add(automation.LongCooldownMoveWindow).UnixMilli())).
			Order(ent.Asc(cooldownclaim.FieldExpiresAtMs)).
			Limit(limit).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	due := make([]CooldownRow, len(rows))
	for i, row := range rows {
		due[i] = CooldownRow{UserID: row.UserID, CooldownHold: CooldownHold{Key: row.CooldownKey, ExpiresAtMS: row.ExpiresAtMs}}
	}
	return due, nil
}
