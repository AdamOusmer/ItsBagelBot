// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strconv"
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

// Never shortens: a marker already reaching the row's expiry or past it stays as it is.
var holdCooldownMarker = pkgvalkey.NewScript(`
if redis.call('PEXPIRETIME', KEYS[1]) >= tonumber(ARGV[1]) then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'PXAT', ARGV[1])
return 1`)

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
	nowMS := time.Now().UnixMilli()
	calls := make([]pkgvalkey.ScriptCall, 0, 2*len(rows))
	for _, row := range rows {
		entry := pkgvalkey.ExpiringIndexEntry{Index: automation.LongCooldownMarkerIndexKey(row.UserID), Member: row.Key, ExpiresAtMS: row.ExpiresAtMS}
		calls = append(calls, entry.AddCall(nowMS), pkgvalkey.ScriptCall{
			Script: holdCooldownMarker,
			Key:    automation.LongCooldownMarkerKey(row.UserID, row.Key),
			Args:   []string{automation.LongCooldownMarkerValue(row.ExpiresAtMS)},
		})
	}
	for _, result := range pkgvalkey.RunScripts(ctx, m.client, calls) {
		if err := result.Error(); err != nil {
			return 0, err
		}
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
	index := automation.LongCooldownMarkerIndexKey(channel)
	for ctx.Err() == nil {
		windows, err := client.Do(ctx, client.B().Zrange().Key(index).Min("0").Max(strconv.Itoa(cooldownEraseBatch-1)).Build()).AsStrSlice()
		if err != nil || len(windows) == 0 {
			return err
		}
		if err := eraseMarkerBatch(ctx, client, channel, windows); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func eraseMarkerBatch(ctx context.Context, client valkey.Client, channel uint64, windows []string) error {
	markers := make([]string, len(windows))
	for i, window := range windows {
		markers[i] = automation.LongCooldownMarkerKey(channel, window)
	}
	index := automation.LongCooldownMarkerIndexKey(channel)
	unlink := client.B().Unlink().Key(markers...).Build()
	forget := client.B().Zrem().Key(index).Member(windows...).Build()
	for _, result := range client.DoMulti(ctx, unlink, forget) {
		if err := result.Error(); err != nil {
			return err
		}
	}
	return nil
}

// The row was released while its marker was being written, so nothing holds that window any more.
func (m *CooldownMover) undoMarker(ctx context.Context, row CooldownRow) {
	key := automation.LongCooldownMarkerKey(row.UserID, row.Key)
	owner := automation.LongCooldownMarkerValue(row.ExpiresAtMS)
	if err := pkgvalkey.NewOwnerLock(m.client, key, owner).Release(ctx); err != nil {
		m.log.Warn("cooldown marker undo failed", zap.String("key", key), zap.Error(err))
	}
}
