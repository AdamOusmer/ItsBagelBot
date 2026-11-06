// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/internal/domain/automation"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	cooldownMoveLeaseKey = "cooldown:long:mover"
	cooldownMoveLease    = time.Hour
	cooldownMovePoll     = 5 * time.Minute
	cooldownMoveBudget   = 5 * time.Minute
	cooldownMoveBatch    = 500
	cooldownEraseBatch   = 500
	cooldownEraseBudget  = 10 * time.Second
)

// CooldownMover hands rows with little time left to Valkey so the table only keeps long waits.
type CooldownMover struct {
	state  *RecipeState
	client valkey.Client
	owner  string
	log    *zap.Logger
}

func NewCooldownMover(state *RecipeState, client valkey.Client, owner string, log *zap.Logger) *CooldownMover {
	return &CooldownMover{state: state, client: client, owner: owner, log: log.Named("cooldown-mover")}
}

// Run polls often and lets the hour-long lease set the pace, so restarts cannot starve the job.
func (m *CooldownMover) Run(ctx context.Context) {
	ticker := time.NewTicker(cooldownMovePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := m.Tick(ctx); err != nil {
				m.log.Warn("cooldown move failed", zap.Error(err))
			}
		}
	}
}

// Tick reports whether this replica held the lease; the lease is kept until it lapses, which is what spaces runs an hour apart.
func (m *CooldownMover) Tick(ctx context.Context) (bool, error) {
	won, err := pkgvalkey.NewOwnerLock(m.client, cooldownMoveLeaseKey, m.owner).Acquire(ctx, cooldownMoveLease)
	if err != nil || !won {
		return false, err
	}
	return true, m.MoveDue(ctx)
}

// MoveDue deletes expired rows and moves due ones. A crash between marker and delete repeats harmlessly next run.
func (m *CooldownMover) MoveDue(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, cooldownMoveBudget)
	defer cancel()
	pruned := m.state.PruneExpiredCooldowns(ctx)
	for ctx.Err() == nil {
		moved, err := m.moveBatch(ctx)
		if err != nil || moved < cooldownMoveBatch {
			return errors.Join(pruned, err)
		}
	}
	return errors.Join(pruned, ctx.Err())
}

func (m *CooldownMover) moveBatch(ctx context.Context) (int, error) {
	rows, err := m.state.dueCooldowns(ctx, time.Now(), cooldownMoveBatch)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	markers := make([]pkgvalkey.IndexedKey, len(rows))
	for i, row := range rows {
		markers[i] = pkgvalkey.IndexedKey{
			Index:       automation.LongCooldownMarkerIndexKey(row.UserID),
			Member:      row.Key,
			Key:         automation.LongCooldownMarkerKey(row.UserID, row.Key),
			Value:       automation.LongCooldownMarkerValue(row.ExpiresAtMS),
			ExpiresAtMS: row.ExpiresAtMS,
		}
	}
	if err := pkgvalkey.HoldIndexedKeys(ctx, m.client, markers); err != nil {
		return 0, err
	}
	return len(rows), m.forgetRows(ctx, rows)
}

func (m *CooldownMover) forgetRows(ctx context.Context, rows []CooldownRow) error {
	for _, row := range rows {
		deleted, err := m.state.deleteCooldownRow(ctx, row)
		if err != nil {
			return err
		}
		if deleted == 0 {
			m.undoMarker(ctx, row)
		}
	}
	return nil
}

// EraseCooldownMarkers walks one channel's index in batches, so it never scans the keyspace. Out of budget it returns the error and the redelivered event carries on.
func EraseCooldownMarkers(ctx context.Context, client valkey.Client, channel uint64) error {
	ctx, cancel := context.WithTimeout(ctx, cooldownEraseBudget)
	defer cancel()
	erase := pkgvalkey.IndexErase{
		Index: automation.LongCooldownMarkerIndexKey(channel),
		Batch: cooldownEraseBatch,
		KeyOf: func(window string) string { return automation.LongCooldownMarkerKey(channel, window) },
	}
	return erase.Run(ctx, client)
}

// The row was released while its marker was being written, so nothing holds that window any more.
func (m *CooldownMover) undoMarker(ctx context.Context, row CooldownRow) {
	key := automation.LongCooldownMarkerKey(row.UserID, row.Key)
	owner := automation.LongCooldownMarkerValue(row.ExpiresAtMS)
	if err := pkgvalkey.NewOwnerLock(m.client, key, owner).Release(ctx); err != nil {
		m.log.Warn("cooldown marker undo failed", zap.String("key", key), zap.Error(err))
	}
}
