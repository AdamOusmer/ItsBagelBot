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
	"ItsBagelBot/app/db/modules/ent/countdown"
	"ItsBagelBot/app/db/modules/ent/countdowndelivery"
	"ItsBagelBot/app/db/modules/ent/countdownoperation"
	"ItsBagelBot/app/db/modules/ent/variable"
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

// Viewer rows can outnumber what a snapshot should hold, so these go by channel rather than by captured identity. A viewer's rows in other channels stay, as Loyalty's balances do.
func (*RecipeState) deleteChannelRows(ctx context.Context, tx *ent.Tx, channel uint64) error {
	if _, err := tx.Variable.Delete().Where(variable.UserID(channel)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.CooldownClaim.Delete().Where(cooldownclaim.UserID(channel)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.Countdown.Delete().Where(countdown.UserID(channel)).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.CountdownDelivery.Delete().Where(countdowndelivery.UserID(channel)).Exec(ctx); err != nil {
		return err
	}
	_, err := tx.CountdownOperation.Delete().Where(countdownoperation.UserID(channel)).Exec(ctx)
	return err
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

// CooldownExtendRequest names the running window to add to; AddSeconds must be positive.
type CooldownExtendRequest struct {
	UserID     uint64
	Key        string
	AddSeconds int64
}

func (req CooldownExtendRequest) valid() error {
	if req.UserID == 0 {
		return fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	if req.Key == "" || len(req.Key) > maxCooldownKeyBytes {
		return fmt.Errorf("%w: cooldown key", ErrInvalidInput)
	}
	if req.AddSeconds <= 0 {
		return fmt.Errorf("%w: cooldown extend seconds", ErrInvalidInput)
	}
	return nil
}

// sqlTxStarter adapts a raw *sql.DB to db.WithTx's TxStarter, the way an ent client's own Tx method does.
type sqlTxStarter struct{ db *sql.DB }

func (s sqlTxStarter) Tx(ctx context.Context) (*sql.Tx, error) { return s.db.BeginTx(ctx, nil) }

// CooldownExtend adds req.AddSeconds to a running window, capped at MaxCooldownSeconds from now; it never shortens
// the window. found is false when the row is gone or already expired. capped marks a result the ceiling shortened.
func (r *RecipeState) CooldownExtend(ctx context.Context, req CooldownExtendRequest) (expiresAtMS int64, found bool, capped bool, err error) {
	if err := req.valid(); err != nil {
		return 0, false, false, err
	}
	err = db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			return db.WithTx(ctx, sqlTxStarter{r.sqldb}, func(tx *sql.Tx) error {
				var txErr error
				expiresAtMS, found, capped, txErr = r.extendCooldownRow(ctx, tx, req)
				return txErr
			})
		})
	})
	if err != nil {
		return 0, false, false, err
	}
	return expiresAtMS, found, capped, nil
}

// extendCooldownRow always reads the row back after the write: MySQL reports zero rows affected when the computed
// value equals the one already stored, which an extend landing exactly on the cap can do to a window that is very
// much still running, so RowsAffected can never be the found signal here.
func (r *RecipeState) extendCooldownRow(ctx context.Context, tx *sql.Tx, req CooldownExtendRequest) (int64, bool, bool, error) {
	nowMS := time.Now().UnixMilli()
	maxMS := int64(automation.MaxCooldownSeconds) * 1000
	if _, err := tx.ExecContext(ctx, r.cooldownExtendQuery(), req.AddSeconds*1000, nowMS, maxMS, req.UserID, req.Key, nowMS); err != nil {
		return 0, false, false, err
	}
	var expiresAtMS int64
	row := tx.QueryRowContext(ctx, "SELECT expires_at_ms FROM cooldown_claims WHERE user_id=? AND cooldown_key=?", req.UserID, req.Key)
	switch err := row.Scan(&expiresAtMS); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, false, nil
	case err != nil:
		return 0, false, false, err
	case expiresAtMS <= nowMS:
		return 0, false, false, nil
	}
	return expiresAtMS, true, expiresAtMS == nowMS+maxMS, nil
}

func (r *RecipeState) cooldownExtendQuery() string {
	set := "expires_at_ms=GREATEST(expires_at_ms, LEAST(expires_at_ms+?, ?+?))"
	if r.dialect == "sqlite3" {
		set = "expires_at_ms=MAX(expires_at_ms, MIN(expires_at_ms+?, ?+?))"
	}
	return "UPDATE cooldown_claims SET " + set + " WHERE user_id=? AND cooldown_key=? AND expires_at_ms>?"
}

// PruneExpiredCooldowns walks the expiry index in bounded batches; it only removes rows a claim would overwrite anyway.
func (r *RecipeState) PruneExpiredCooldowns(ctx context.Context) error {
	return r.pruneBefore(ctx, pruneTarget{table: "cooldown_claims", column: "expires_at_ms"}, time.Now().UnixMilli()+1)
}

// PruneCountdownOperations drops retry identities older than 31 days.
func (r *RecipeState) PruneCountdownOperations(ctx context.Context) error {
	return r.pruneBefore(ctx, pruneTarget{table: "countdown_operations", column: "created_at_ms"}, time.Now().Add(-31*24*time.Hour).UnixMilli())
}

type pruneTarget struct{ table, column string }

func (r *RecipeState) pruneBefore(ctx context.Context, target pruneTarget, beforeMS int64) error {
	query := r.pruneQuery(target)
	for ctx.Err() == nil {
		deleted, err := r.pruneBatch(ctx, query, beforeMS)
		if err != nil || deleted < cooldownPruneBatch {
			return err
		}
	}
	return ctx.Err()
}

func (r *RecipeState) pruneBatch(ctx context.Context, query string, beforeMS int64) (int64, error) {
	var deleted int64
	err := db.WithExec(ctx, func(ctx context.Context) error {
		result, err := r.sqldb.ExecContext(ctx, query, beforeMS, cooldownPruneBatch)
		if err != nil {
			return err
		}
		deleted, err = result.RowsAffected()
		return err
	})
	return deleted, err
}

func (r *RecipeState) pruneQuery(target pruneTarget) string {
	expired := " WHERE " + target.column + "<? ORDER BY " + target.column + " LIMIT ?"
	if r.dialect == "sqlite3" {
		return "DELETE FROM " + target.table + " WHERE id IN (SELECT id FROM " + target.table + expired + ")"
	}
	return "DELETE FROM " + target.table + expired
}

// CooldownRow is one stored window due to leave the database.
type CooldownRow struct {
	UserID uint64
	CooldownHold
}

// dueCooldowns never offers a named window: its row stays the truth for its whole life, since cooldown_add can only extend what the database still holds.
func (r *RecipeState) dueCooldowns(ctx context.Context, now time.Time, limit int) ([]CooldownRow, error) {
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.CooldownClaim, error) {
		return r.client.CooldownClaim.Query().
			Where(
				cooldownclaim.ExpiresAtMsGT(now.UnixMilli()),
				cooldownclaim.ExpiresAtMsLTE(now.Add(automation.LongCooldownMoveWindow).UnixMilli()),
				cooldownclaim.Not(cooldownclaim.CooldownKeyHasPrefix(automation.LongCooldownNamedPrefix)),
			).
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
