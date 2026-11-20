// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"github.com/stretchr/testify/require"
)

func seedGiveawayWinners(t *testing.T) (*repository.GiveawayWinners, *repository.Modules, time.Time) {
	t.Helper()
	client, _, modules := setup(t)
	t.Cleanup(func() { modules.Close(t.Context()) })
	repo := repository.NewGiveawayWinners(client)
	cutoff := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	rows := []repository.WinnerRow{
		{WinnerRef: repository.WinnerRef{RoundID: "round-a", Slot: 1}, ViewerID: 10, Login: "viewer", Prize: "headset", WonAt: cutoff.Add(-time.Millisecond)},
		{WinnerRef: repository.WinnerRef{RoundID: "round-a", Slot: 2}, ViewerID: 20, Login: "regular", Prize: "headset", WonAt: cutoff},
		{WinnerRef: repository.WinnerRef{RoundID: "round-b", Slot: 1}, ViewerID: 20, Login: "renamed", Prize: "keyboard", WonAt: cutoff, ClaimedAt: &cutoff},
	}
	require.NoError(t, repo.Record(t.Context(), 1, rows[:2]))
	require.NoError(t, repo.Record(t.Context(), 1, rows[2:]))
	require.NoError(t, repo.Record(t.Context(), 2, rows[:1]))
	return repo, modules, cutoff
}

func TestGiveawayWinnerHistory(t *testing.T) {
	repo, _, cutoff := seedGiveawayWinners(t)
	for _, tc := range []struct {
		name string
		q    modulesrpc.GiveawayRecentQuery
		want []string
	}{
		{name: "newest first", want: []string{"renamed", "regular", "viewer"}},
		{name: "first page", q: modulesrpc.GiveawayRecentQuery{Limit: 1}, want: []string{"renamed"}},
		{name: "next page", q: modulesrpc.GiveawayRecentQuery{Limit: 2, Offset: 1}, want: []string{"regular", "viewer"}},
		{name: "round filter", q: modulesrpc.GiveawayRecentQuery{RoundID: "round-a"}, want: []string{"regular", "viewer"}},
		{name: "empty page", q: modulesrpc.GiveawayRecentQuery{Offset: 3}, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := repo.Recent(t.Context(), 1, tc.q)
			require.NoError(t, err)
			logins := make([]string, len(rows))
			for i, row := range rows {
				logins[i] = row.Login
			}
			require.Equal(t, tc.want, logins)
		})
	}
	ids, err := repo.WonWithin(t.Context(), 1, []uint64{10, 20, 20, 30}, cutoff)
	require.NoError(t, err)
	require.Equal(t, []uint64{20}, ids)
}

func TestGiveawayWinnerDeliveryAndReplay(t *testing.T) {
	repo, _, cutoff := seedGiveawayWinners(t)
	ctx := t.Context()
	ref := repository.WinnerRef{RoundID: "round-b", Slot: 1}
	delivery := repository.WinnerDelivery{WinnerRef: ref, Delivered: true, Note: "Handed over on stream"}
	found, err := repo.MarkDelivered(ctx, 2, delivery)
	require.NoError(t, err)
	require.False(t, found)

	found, err = repo.MarkDelivered(ctx, 1, delivery)
	require.NoError(t, err)
	require.True(t, found)
	replay := repository.WinnerRow{WinnerRef: ref, ViewerID: 30, Login: "other", Prize: "other", WonAt: cutoff}
	require.NoError(t, repo.Record(ctx, 1, []repository.WinnerRow{replay}))
	winner := readRoundWinner(t, repo)
	require.Positive(t, winner.DeliveredAt)
	require.Equal(t, modulesrpc.GiveawayWinner{
		RoundID: "round-b", Slot: 1, ViewerID: "20", Login: "renamed", Prize: "keyboard",
		WonAt: cutoff.UnixMilli(), Claimed: true, ClaimedAt: cutoff.UnixMilli(),
		DeliveredAt: winner.DeliveredAt, DeliveryNote: delivery.Note,
	}, winner)

	found, err = repo.MarkDelivered(ctx, 1, repository.WinnerDelivery{WinnerRef: ref})
	require.NoError(t, err)
	require.True(t, found)
	winner = readRoundWinner(t, repo)
	require.Zero(t, winner.DeliveredAt)
	require.Empty(t, winner.DeliveryNote)
}

func readRoundWinner(t *testing.T, repo *repository.GiveawayWinners) modulesrpc.GiveawayWinner {
	t.Helper()
	winners, err := repo.Recent(t.Context(), 1, modulesrpc.GiveawayRecentQuery{RoundID: "round-b"})
	require.NoError(t, err)
	require.Len(t, winners, 1)
	return winners[0]
}

func TestDeleteAccountRemovesCapturedGiveawayWinners(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current int64
		want    int
	}{
		{name: "deleted account", want: 0},
		{name: "replacement account", current: 101, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, modules, _ := seedGiveawayWinners(t)
			modules.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return tc.current, nil })
			require.NoError(t, modules.DeleteAccount(t.Context(), 1, 100))
			for userID, count := range map[uint64]int{1: tc.want, 2: 1} {
				winners, err := repo.Recent(t.Context(), userID, modulesrpc.GiveawayRecentQuery{})
				require.NoError(t, err)
				require.Len(t, winners, count)
			}
		})
	}
}

func TestDeleteAccountPreservesGiveawayWinnersWrittenAfterCapture(t *testing.T) {
	repo, modules, cutoff := seedGiveawayWinners(t)
	modules.SetAccountInstanceResolver(func(ctx context.Context, userID uint64) (int64, error) {
		newWinner := repository.WinnerRow{
			WinnerRef: repository.WinnerRef{RoundID: "new-round", Slot: 1},
			ViewerID:  30, Prize: "headset", WonAt: cutoff,
		}
		return 0, repo.Record(ctx, userID, []repository.WinnerRow{newWinner})
	})
	require.NoError(t, modules.DeleteAccount(t.Context(), 1, 100))
	winners, err := repo.Recent(t.Context(), 1, modulesrpc.GiveawayRecentQuery{})
	require.NoError(t, err)
	require.Len(t, winners, 1)
	require.Equal(t, "new-round", winners[0].RoundID)
}
