// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func numberedViewers(n int) []chattersSnapshotEntry {
	entries := make([]chattersSnapshotEntry, n)
	for i := range entries {
		entries[i] = chattersSnapshotEntry{ID: uint64(i + 1), Login: "v", Name: "v"}
	}
	return entries
}

func TestValkeyChattersSnapshot(t *testing.T) {
	pair := []chattersSnapshotEntry{{ID: 1, Login: "sam", Name: "sam"}, {ID: 2, Login: "alex", Name: "alex"}}
	cases := []struct {
		name      string
		stored    []chattersSnapshotEntry
		raw       string
		advance   time.Duration
		breakGets bool
		want      []chattersSnapshotEntry
		wantErr   bool
	}{
		{name: "nothing stored is a clean miss"},
		{name: "a stored snapshot round trips", stored: pair, want: pair},
		{
			name: "the stored list is capped", stored: numberedViewers(chattersSnapshotCap + 50),
			want: numberedViewers(chattersSnapshotCap),
		},
		{name: "a snapshot is still served inside the TTL window", stored: pair, advance: chattersSnapshotTTL - time.Second, want: pair},
		{name: "a snapshot expires past the TTL window", stored: pair, advance: chattersSnapshotTTL + time.Second},
		{name: "a transport failure is distinguishable from a miss", breakGets: true, wantErr: true},
		{name: "an undecodable value reports an error", raw: "not json", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeValkey(t)
			store := NewValkeyChatters(f.client, zap.NewNop())
			ctx := context.Background()
			if tc.stored != nil {
				store.Store(ctx, 123, tc.stored)
			}
			if tc.raw != "" {
				require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key(chattersSnapshotKey(123)).Value(tc.raw).Build()).Error())
			}
			if tc.breakGets {
				f.breakReads()
			}
			f.advance(tc.advance)

			got, ok, err := store.Snapshot(ctx, 123)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want != nil, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValkeyChattersFetchLockContentionAndRelease(t *testing.T) {
	f := newFakeValkey(t)
	store := NewValkeyChatters(f.client, zap.NewNop())
	ctx := context.Background()

	assert.True(t, store.TryFetchLock(ctx, 123), "the first caller wins the lock")
	assert.False(t, store.TryFetchLock(ctx, 123), "a second caller must not win the same lock")

	store.ReleaseFetchLock(ctx, 123)
	assert.True(t, store.TryFetchLock(ctx, 123), "a released lock is claimable again immediately")
}
