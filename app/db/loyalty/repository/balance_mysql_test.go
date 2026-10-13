// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	balanceReplicas = 12
	balanceRounds   = 30
	balanceCreated  = 100
	balanceSender   = 1 << 40
)

type balanceWriter struct {
	repo    *Loyalty
	channel uint64
}

func (w balanceWriter) recipient(round int) uint64 { return uint64(1000 - round) }

func (w balanceWriter) adjusted(round int) uint64 { return uint64(2000 + round) }

func (w balanceWriter) watcher(round int) uint64 { return uint64(3000 + round) }

func (w balanceWriter) earner(round int) uint64 { return uint64(2*balanceSender + round) }

func (w balanceWriter) award(round int) data.WatchAwardDTO {
	return data.WatchAwardDTO{
		UserID: w.channel, AccountCreatedAt: balanceCreated, Generation: "1", LiveSession: "live", WindowID: fmt.Sprint("w", round),
		Entries: []data.LoyaltyEarnEntry{
			{ViewerID: balanceSender, ViewerLogin: "sender", Points: 10, WatchSeconds: 60},
			{ViewerID: w.watcher(round), ViewerLogin: fmt.Sprint("watcher", round), Points: 10, WatchSeconds: 60},
		},
	}
}

func (w balanceWriter) round(ctx context.Context, round int) error {
	if err := w.repo.ApplyWatchAward(ctx, w.award(round)); err != nil {
		return fmt.Errorf("watch award: %w", err)
	}
	transfer := Transfer{UserID: w.channel, FromViewerID: balanceSender, TargetLogin: fmt.Sprint("recipient", round), TargetViewerID: w.recipient(round), Amount: 1}
	if _, _, err := w.repo.BalanceTransfer(ctx, transfer); err != nil {
		return fmt.Errorf("transfer: %w", err)
	}
	adjust := BalanceAdjustment{UserID: w.channel, ViewerID: w.adjusted(round), ViewerLogin: fmt.Sprint("adjusted", round), Value: 5}
	if _, _, err := w.repo.BalanceAdjustViewer(ctx, adjust); err != nil {
		return fmt.Errorf("adjust: %w", err)
	}
	w.repo.RecordEarned(data.LoyaltyEarnedDTO{UserID: w.channel, Entries: []data.LoyaltyEarnEntry{{ViewerID: w.earner(round), ViewerLogin: fmt.Sprint("earner", round), Points: 3}}})
	w.repo.Flush(ctx)
	return nil
}

func (w balanceWriter) run(errs chan<- error) {
	for round := range balanceRounds {
		errs <- w.round(context.Background(), round)
	}
}

func TestBalanceMySQLReplicasPostOtherChannelsWithoutLockConflicts(t *testing.T) {
	raw := testdb.MySQL(t, "bagel_balance_")
	replicas := make([]*Loyalty, balanceReplicas)
	for i := range replicas {
		replicas[i] = mysqlReplica(t, raw)
	}
	before := readInnodbLocks(t, raw)
	errs := make(chan error, balanceReplicas*balanceRounds)
	var writers sync.WaitGroup
	for replica, repo := range replicas {
		writers.Go(func() { balanceWriter{repo: repo, channel: uint64(1 + replica)}.run(errs) })
	}
	writers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	after := readInnodbLocks(t, raw)

	assert.Zero(t, after.deadlocks-before.deadlocks, "replicas posting to different channels deadlocked")
	assert.Zero(t, after.waits-before.waits, "replicas posting to different channels waited on each other's row locks")
	var rows, points int64
	require.NoError(t, raw.QueryRow("SELECT COUNT(*), SUM(points) FROM balances").Scan(&rows, &points))
	channels := int64(balanceReplicas)
	assert.EqualValues(t, channels*(1+4*balanceRounds), rows)
	assert.EqualValues(t, channels*balanceRounds*(10+10+5+3), points)
}
