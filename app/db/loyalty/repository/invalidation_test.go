// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"slices"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureInvalidations(repo *loyaltyrepo.Loyalty) *[]cache.Event {
	events := new([]cache.Event)
	h := bus.NewInvalidations(nil, "", nil)
	for _, scope := range []string{invalidate.ScopeLoyalty, invalidate.ScopeLoyaltyCounters} {
		h.Watch(scope, func(e cache.Event) {
			e.Keys = slices.Clone(e.Keys)
			slices.Sort(e.Keys)
			*events = append(*events, e)
		})
	}
	repo.SetInvalidations(h)
	return events
}

func TestBalanceMutationsInvalidateDurableViewers(t *testing.T) {
	for _, operation := range []string{"set", "add", "debit", "win", "loss", "transfer"} {
		t.Run(operation, func(t *testing.T) {
			repo, client := newLoyaltyRepo(t)
			seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 100})
			h := bus.NewInvalidations(nil, "", nil)
			var snapshots [][]int64
			h.Watch(invalidate.ScopeLoyalty, func(e cache.Event) {
				var points []int64
				for _, key := range e.Keys {
					viewer := uint64(7)
					if key == "8" {
						viewer = 8
					}
					row, found, err := repo.BalanceGet(t.Context(), e.ID, viewer)
					require.NoError(t, err)
					require.True(t, found)
					points = append(points, row.Points)
				}
				snapshots = append(snapshots, points)
			})
			repo.SetInvalidations(h)
			want := mutateBalance(t, repo, operation)
			assert.Equal(t, [][]int64{want}, snapshots)
		})
	}
}

func mutateBalance(t *testing.T, repo *loyaltyrepo.Loyalty, operation string) []int64 {
	t.Helper()
	ctx := t.Context()
	switch operation {
	case "set", "add":
		_, _, err := repo.BalanceAdjustViewer(ctx, loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 7, ViewerLogin: "sender", Value: 20, Absolute: operation == "set"})
		require.NoError(t, err)
		if operation == "set" {
			return []int64{20}
		}
		return []int64{120}
	case "debit":
		require.NoError(t, repo.EnsureBalanceCreditSchema(ctx))
		_, err := repo.BalanceDebit(ctx, loyaltyrepo.BalanceDebit{UserID: 2, ViewerID: 7, Value: 20, Key: "recipe:r:1"})
		require.NoError(t, err)
		return []int64{80}
	case "win", "loss":
		_, err := repo.BalanceWager(ctx, loyaltyrepo.Wager{UserID: 2, ViewerID: 7, Amount: 20, Won: operation == "win"})
		require.NoError(t, err)
		if operation == "win" {
			return []int64{120}
		}
		return []int64{80}
	default:
		_, _, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "receiver", Amount: 20})
		require.NoError(t, err)
		return []int64{80, 20}
	}
}

func TestRefusedBalanceMutationsDoNotInvalidate(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	events := captureInvalidations(repo)
	_, err := repo.BalanceWager(t.Context(), loyaltyrepo.Wager{UserID: 2, ViewerID: 7, Amount: 20})
	require.NoError(t, err)
	_, _, err = repo.BalanceTransfer(t.Context(), loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "receiver", Amount: 20})
	require.NoError(t, err)
	assert.Empty(t, *events)
}

func TestEarnedAndWatchAwardsInvalidateOneViewerBatch(t *testing.T) {
	for _, operation := range []string{"earned", "watch"} {
		t.Run(operation, func(t *testing.T) {
			repo, _ := sqliteRetentionRepo(t)
			events := captureInvalidations(repo)
			entries := []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 1, WatchSeconds: 30}, {ViewerID: 8, WatchSeconds: 30}}
			if operation == "earned" {
				entries = append(entries, data.LoyaltyEarnEntry{ViewerID: 7, Points: 2}, data.LoyaltyEarnEntry{Points: 1}, data.LoyaltyEarnEntry{ViewerID: 9})
				require.NoError(t, repo.ApplyEarned(t.Context(), data.LoyaltyEarnedDTO{BatchID: "batch", UserID: 17, Entries: entries}))
			} else {
				award := watchAward(17, 7, 100)
				award.Entries = entries
				require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
			}
			assert.Equal(t, []cache.Event{{Scope: invalidate.ScopeLoyalty, ID: 17, Keys: []string{"7", "8"}}}, *events)
		})
	}
}

func TestFailedEarnedAndWatchWritesDoNotInvalidate(t *testing.T) {
	repo, raw := sqliteRetentionRepo(t)
	_, err := raw.Exec("CREATE TRIGGER fail_balance BEFORE INSERT ON balances BEGIN SELECT RAISE(ABORT, 'write failed'); END")
	require.NoError(t, err)
	events := captureInvalidations(repo)
	award := watchAward(17, 7, 100)
	require.Error(t, repo.ApplyWatchAward(t.Context(), award))
	require.Error(t, repo.ApplyEarned(t.Context(), data.LoyaltyEarnedDTO{BatchID: "failed", UserID: 17, Entries: award.Entries}))
	assert.Empty(t, *events)
}

func TestEmptyEarnedWindowDoesNotInvalidate(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	events := captureInvalidations(repo)
	require.NoError(t, repo.ApplyEarned(t.Context(), data.LoyaltyEarnedDTO{BatchID: "empty", UserID: 17, Entries: []data.LoyaltyEarnEntry{{Points: 1}, {ViewerID: 7}}}))
	assert.Empty(t, *events)
}

func TestDuplicateAndRetiredWatchAwardsDoNotInvalidate(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	award := watchAward(17, 7, 100)
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	events := captureInvalidations(repo)
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	assert.Empty(t, *events)
	require.NoError(t, repo.DeleteAccount(t.Context(), 17, 100))
	*events = nil
	award = watchAward(17, 8, 100)
	award.WindowID = "retired-window"
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	assert.Empty(t, *events)
}

func TestAccountChangesInvalidateWholeChannel(t *testing.T) {
	for _, operation := range []string{"delete", "restore", "watch-recreation"} {
		t.Run(operation, func(t *testing.T) {
			repo, _ := sqliteRetentionRepo(t)
			require.NoError(t, repo.RestoreUser(t.Context(), 17, 100))
			require.NoError(t, repo.ApplyWatchAward(t.Context(), watchAward(17, 7, 100)))
			events := captureInvalidations(repo)
			switch operation {
			case "delete":
				require.NoError(t, repo.DeleteAccount(t.Context(), 17, 100))
			case "restore":
				require.NoError(t, repo.RestoreUser(t.Context(), 17, 200))
			default:
				require.NoError(t, repo.ApplyWatchAward(t.Context(), watchAward(17, 8, 200)))
			}
			want := []cache.Event{{Scope: invalidate.ScopeLoyalty, ID: 17, Keys: []string{}}, {Scope: invalidate.ScopeLoyaltyCounters, ID: 17}}
			if operation == "watch-recreation" {
				want = append(want, cache.Event{Scope: invalidate.ScopeLoyalty, ID: 17, Keys: []string{"8"}})
			}
			assert.Equal(t, want, *events)
		})
	}
}

func TestEarnedInvalidationRunsOutsidePersistenceLock(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	h := bus.NewInvalidations(nil, "", nil)
	h.Watch(invalidate.ScopeLoyalty, func(e cache.Event) {
		if e.ID == 17 {
			require.NoError(t, repo.DeleteAccount(context.Background(), 18, 100))
		}
	})
	repo.SetInvalidations(h)
	require.NoError(t, repo.ApplyEarned(t.Context(), data.LoyaltyEarnedDTO{BatchID: "lock", UserID: 17, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 1}}}))
}
