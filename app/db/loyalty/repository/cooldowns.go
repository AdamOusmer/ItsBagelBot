// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/cooldownclaim"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

const (
	maxCooldownWindows   = 2
	maxCooldownKeyBytes  = 128
	maxCooldownTTL       = automation.MaxCooldownSeconds * time.Second
	cooldownPruneBatch   = 1000
	cooldownPruneBudget  = 2 * time.Second
	cooldownReleaseGrace = 5 * time.Second
)

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

// CooldownHold names one won window; its expiry tells this claim apart from any later one on the same key.
type CooldownHold struct {
	Key         string
	ExpiresAtMS int64
}

// CooldownClaimAll wins every window or none, claiming in the given order.
func (r *Loyalty) CooldownClaimAll(ctx context.Context, claim CooldownClaim) ([]CooldownHold, bool, error) {
	if err := claim.valid(); err != nil {
		return nil, false, err
	}
	holds := make([]CooldownHold, 0, len(claim.Windows))
	for _, window := range claim.Windows {
		hold, won, err := r.claimCooldownWindow(ctx, claim.UserID, window)
		if err != nil || !won {
			return nil, false, errors.Join(err, r.releaseAfterRefusal(ctx, claim.UserID, holds))
		}
		holds = append(holds, hold)
	}
	return holds, true, nil
}

func (r *Loyalty) releaseAfterRefusal(ctx context.Context, userID uint64, holds []CooldownHold) error {
	if len(holds) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cooldownReleaseGrace)
	defer cancel()
	return r.CooldownRelease(ctx, userID, holds)
}

func (r *Loyalty) claimCooldownWindow(ctx context.Context, userID uint64, window CooldownWindow) (CooldownHold, bool, error) {
	now := time.Now()
	hold := CooldownHold{Key: window.Key, ExpiresAtMS: now.Add(window.TTL).UnixMilli()}
	won := false
	err := retryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			var err error
			won, err = r.takeCooldownRow(ctx, userID, hold, now.UnixMilli())
			return err
		})
	})
	return hold, won, err
}

// Two autocommit statements, never one transaction: a failed insert keeps a shared lock on the row until commit, and two of those deadlock on the next write.
func (r *Loyalty) takeCooldownRow(ctx context.Context, userID uint64, hold CooldownHold, nowMS int64) (bool, error) {
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
func (r *Loyalty) CooldownRelease(ctx context.Context, userID uint64, holds []CooldownHold) error {
	if userID == 0 {
		return fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	var failed error
	for _, hold := range holds {
		failed = errors.Join(failed, r.releaseCooldownRow(ctx, userID, hold))
	}
	return failed
}

func (r *Loyalty) releaseCooldownRow(ctx context.Context, userID uint64, hold CooldownHold) error {
	return retryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			_, err := r.client.CooldownClaim.Delete().
				Where(cooldownclaim.UserID(userID), cooldownclaim.CooldownKey(hold.Key), cooldownclaim.ExpiresAtMs(hold.ExpiresAtMS)).
				Exec(ctx)
			return err
		})
	})
}

// PruneExpiredCooldowns walks the expiry index in bounded batches; it only removes rows a claim would overwrite anyway.
func (r *Loyalty) PruneExpiredCooldowns(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, cooldownPruneBudget)
	defer cancel()
	now := time.Now().UnixMilli()
	for ctx.Err() == nil {
		deleted, err := r.pruneCooldownBatch(ctx, now)
		if err != nil || deleted < cooldownPruneBatch {
			return err
		}
	}
	return nil
}

func (r *Loyalty) pruneCooldownBatch(ctx context.Context, nowMS int64) (int64, error) {
	result, err := r.sqldb.ExecContext(ctx, r.cooldownPruneQuery(), nowMS, cooldownPruneBatch)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *Loyalty) cooldownPruneQuery() string {
	expired := " WHERE expires_at_ms<=? ORDER BY expires_at_ms LIMIT ?"
	if r.dialect == "sqlite3" {
		return "DELETE FROM cooldown_claims WHERE id IN (SELECT id FROM cooldown_claims" + expired + ")"
	}
	return "DELETE FROM cooldown_claims" + expired
}

func (r *Loyalty) pruneCooldownsOnTick() {
	err := r.PruneExpiredCooldowns(context.Background())
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		r.log.Warn("loyalty: expired cooldown cleanup failed", zap.Error(err))
	}
}
