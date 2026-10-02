// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func raffleStoreFor(t *testing.T) (*ValkeyRaffleStore, uint64) {
	t.Helper()
	client := newHotPathTestClient(t)
	id := freshChannel()
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(
			raffleKey(raffleDeadlinePrefix, id), raffleKey(raffleStatePrefix, id), raffleKey(raffleEntriesPrefix, id),
			raffleKey(raffleRemindPrefix, id), raffleKey(raffleDrawPrefix, id), raffleKey(raffleLastPrefix, id),
		).Build())
	})
	return NewValkeyRaffleStore(client, RaffleConfig{}, nil), id
}

func numberedEntrants(n int) []string {
	entrants := make([]string, n)
	for i := range entrants {
		entrants[i] = fmt.Sprintf("viewer%03d", i)
	}
	return entrants
}

func TestValkeyRaffleDrawsDistinctWinnersFromTheEntrants(t *testing.T) {
	cases := []struct {
		name        string
		opened      int64
		entrants    int
		ask         int64
		wantWinners int
	}{
		{"the opening spec decides the winner count when the draw does not override it", 3, 10, 0, 3},
		{"a draw override beats the opening spec", 3, 10, 5, 5},
		{"an ask beyond the pool draws every entrant once", 3, 4, 50, 4},
		{"an unset opening spec draws a single winner", 0, 6, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, id := raffleStoreFor(t)
			ctx := context.Background()
			opened, err := store.Open(ctx, id, RaffleOpenSpec{OpenedBy: "streamer", Winners: tc.opened, Duration: time.Minute})
			require.NoError(t, err)
			require.True(t, opened)
			entrants := numberedEntrants(tc.entrants)
			for _, viewer := range entrants {
				entry, err := store.Join(ctx, id, viewer)
				require.NoError(t, err)
				require.True(t, entry.Joined)
			}

			res, err := store.Draw(ctx, id, tc.ask)

			require.NoError(t, err)
			require.NotNil(t, res)
			assert.Len(t, res.Winners, tc.wantWinners)
			assert.Subset(t, entrants, res.Winners, "winners come from the eligible entries")
			assert.Len(t, uniqueStrings(res.Winners), tc.wantWinners, "no entrant wins twice")
			assert.EqualValues(t, tc.entrants, res.Entrants)
			assert.Equal(t, DigestPool(entrants), res.Digest, "the digest binds the exact pool in join order")
		})
	}
}

func uniqueStrings(items []string) map[string]bool {
	set := map[string]bool{}
	for _, item := range items {
		set[item] = true
	}
	return set
}

func TestValkeyRaffleClosedRaffleIsInert(t *testing.T) {
	store, id := raffleStoreFor(t)
	ctx := context.Background()

	entry, err := store.Join(ctx, id, "early")
	require.NoError(t, err)
	drawn, err := store.Draw(ctx, id, 0)
	require.NoError(t, err)

	assert.False(t, entry.Open, "joining a closed raffle does nothing")
	assert.Nil(t, drawn, "a closed raffle has nothing to draw")
}

func TestValkeyRaffleLifecycle(t *testing.T) {
	store, id := raffleStoreFor(t)
	ctx := context.Background()

	opened, err := store.Open(ctx, id, RaffleOpenSpec{OpenedBy: "streamer", Winners: 1, Duration: time.Minute})
	require.NoError(t, err)
	require.True(t, opened)
	reopened, err := store.Open(ctx, id, RaffleOpenSpec{OpenedBy: "other", Duration: time.Minute})
	require.NoError(t, err)
	assert.False(t, reopened, "a second open while one is running is refused")

	first, err := store.Join(ctx, id, "sam")
	require.NoError(t, err)
	again, err := store.Join(ctx, id, "sam")
	require.NoError(t, err)
	assert.Equal(t, RaffleEntry{Joined: true, Open: true, Entrants: 1}, first)
	assert.Equal(t, RaffleEntry{Joined: false, Open: true, Entrants: 1}, again, "a repeat join is not counted twice")
	status, err := store.Status(ctx, id)
	require.NoError(t, err)
	assert.True(t, status.Open)
	assert.EqualValues(t, 1, status.Entrants)

	res, err := store.Draw(ctx, id, 0)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, []string{"sam"}, res.Winners)

	last, found, err := store.LastResult(ctx, id)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, res.Winners, last.Winners)
	closed, err := store.Status(ctx, id)
	require.NoError(t, err)
	assert.False(t, closed.Open, "a drawn raffle is closed")
	t.Cleanup(func() {
		client := newHotPathTestClient(t)
		client.Do(ctx, client.B().Del().Key(raffleSnapPrefix+strconv.FormatUint(id, 10)+":"+strconv.FormatInt(res.DrawnAt, 10)).Build())
	})
}
