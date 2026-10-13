// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

const lockKey = "test:lock"

func TestKeyspaceHelpersReportBackendFailure(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(valkey_go.Client) (bool, error)
	}{
		{"Incr", func(c valkey_go.Client) (bool, error) {
			_, err := Incr(ctx, c, "k", time.Minute)
			return false, err
		}},
		{"owner lock acquire", func(c valkey_go.Client) (bool, error) {
			return NewOwnerLock(c, lockKey, "pod-a").Acquire(ctx, time.Minute)
		}},
		{"owner lock renew", func(c valkey_go.Client) (bool, error) {
			return NewOwnerLock(c, lockKey, "pod-a").Renew(ctx, time.Minute)
		}},
		{"claim once", func(c valkey_go.Client) (bool, error) {
			return ClaimOnce(ctx, c, "test:claim", 30*time.Second)
		}},
		{"record recent", func(c valkey_go.Client) (bool, error) {
			err := RecordRecent(ctx, c, RecentLog{Key: "k", Keep: 100, TTL: time.Hour}, time.Now())
			return false, err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeValkey(t)
			f.breakBackend()

			won, err := tc.call(f.client)

			require.Error(t, err)
			assert.False(t, won)
		})
	}
}

func TestIncrCountsAndRefreshesTTLOnEveryCall(t *testing.T) {
	f := newFakeValkey(t)
	ctx := context.Background()

	first, err := Incr(ctx, f.client, "k", 48*time.Hour)
	require.NoError(t, err)
	second, err := Incr(ctx, f.client, "k", 48*time.Hour)
	require.NoError(t, err)
	read, err := GetInt(ctx, f.client, "k")
	require.NoError(t, err)

	assert.Equal(t, []int64{1, 2, 2}, []int64{first, second, read})
	assert.WithinDuration(t, f.now.Add(48*time.Hour), f.deadline("k"), time.Second,
		"EXPIRE must re-apply on every Incr, not just the key's creation")
}

func TestGetIntMissReadsAsZero(t *testing.T) {
	f := newFakeValkey(t)

	n, err := GetInt(context.Background(), f.client, "never-incremented")

	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestKVSetCarriesTTLOnlyWhenPositive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     Key
		wantSet []string
	}{
		{"a ttl becomes EX seconds", Key{Name: "k", TTL: time.Minute}, []string{"SET", "k", "v", "EX", "60"}},
		{"no ttl is persistent", Key{Name: "k"}, []string{"SET", "k", "v"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeValkey(t)
			kv := NewKV(f.client)
			ctx := context.Background()

			require.NoError(t, kv.Set(ctx, tc.key, "v"))
			got, ok := kv.GetString(ctx, "k")

			assert.True(t, ok)
			assert.Equal(t, "v", got)
			assert.Equal(t, tc.wantSet, f.lastSet())
		})
	}
}

func TestKVGetStringTreatsAbsentAndEmptyAsMissAndDeleteRemoves(t *testing.T) {
	f := newFakeValkey(t)
	kv := NewKV(f.client)
	ctx := context.Background()
	f.put("empty", "")
	require.NoError(t, kv.Set(ctx, Key{Name: "doomed"}, "v"))
	require.NoError(t, kv.Del(ctx, "doomed"))

	_, absent := kv.GetString(ctx, "absent")
	_, empty := kv.GetString(ctx, "empty")
	_, deleted := kv.GetString(ctx, "doomed")

	assert.Equal(t, []bool{false, false, false}, []bool{absent, empty, deleted},
		"a stored empty string is indistinguishable from a miss")
}

func TestKVJSONRoundTripAndUndecodableValue(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	f := newFakeValkey(t)
	kv := NewKV(f.client)
	ctx := context.Background()
	f.put("garbage", "{not json")

	require.NoError(t, SetJSON(ctx, kv, Key{Name: "blob", TTL: time.Hour}, payload{Name: "desk", Count: 3}))
	got, ok := GetJSON[payload](ctx, kv, "blob")
	fallback, decoded := GetJSON[payload](ctx, kv, "garbage")

	assert.True(t, ok)
	assert.Equal(t, payload{Name: "desk", Count: 3}, got)
	assert.False(t, decoded, "a blob from a build that no longer exists reads as a miss")
	assert.Equal(t, payload{}, fallback)
}

func TestOwnerLockIsExclusiveAndReleasedOnlyByItsOwner(t *testing.T) {
	f := newFakeValkey(t)
	ctx := context.Background()
	holder := NewOwnerLock(f.client, lockKey, "pod-a")
	other := NewOwnerLock(f.client, lockKey, "pod-b")

	won, err := holder.Acquire(ctx, time.Minute)
	require.NoError(t, err)
	assert.True(t, won)

	won, err = other.Acquire(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, won, "a contended acquire loses without an error")

	require.NoError(t, other.Release(ctx))
	owner, held := f.value(lockKey)
	assert.True(t, held)
	assert.Equal(t, "pod-a", owner, "a foreign release must leave the lock with its holder")

	require.NoError(t, holder.Release(ctx))
	won, err = other.Acquire(ctx, time.Minute)
	require.NoError(t, err)
	assert.True(t, won, "the lock is free once its owner releases it")
}

func TestOwnerLockRenewExtendsOnlyTheOwnersLease(t *testing.T) {
	f := newFakeValkey(t)
	ctx := context.Background()
	holder := NewOwnerLock(f.client, lockKey, "pod-a")
	other := NewOwnerLock(f.client, lockKey, "pod-b")

	kept, err := holder.Renew(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, kept, "renewing a lock nobody holds must not create it")

	won, err := holder.Acquire(ctx, 10*time.Second)
	require.NoError(t, err)
	require.True(t, won)

	kept, err = other.Renew(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, kept, "a foreign renew must not extend the holder's lock")

	f.advance(8 * time.Second)
	kept, err = holder.Renew(ctx, 10*time.Second)
	require.NoError(t, err)
	assert.True(t, kept)

	f.advance(8 * time.Second)
	won, err = other.Acquire(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, won, "the renewed lease outlives the original TTL")

	f.advance(3 * time.Second)
	kept, err = holder.Renew(ctx, 10*time.Second)
	require.NoError(t, err)
	assert.False(t, kept, "a lapsed lease cannot be renewed")
}

func TestOwnerLockReleaseOfAbsentKeyIsNoError(t *testing.T) {
	f := newFakeValkey(t)

	require.NoError(t, NewOwnerLock(f.client, lockKey, "pod-a").Release(context.Background()))
}

func TestOwnerLockAcquireExpiresWithMillisecondPrecision(t *testing.T) {
	f := newFakeValkey(t)
	ctx := context.Background()
	holder := NewOwnerLock(f.client, lockKey, "pod-a")
	other := NewOwnerLock(f.client, lockKey, "pod-b")

	won, err := holder.Acquire(ctx, 1500*time.Millisecond)
	require.NoError(t, err)
	require.True(t, won)

	f.advance(time.Second)
	won, err = other.Acquire(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, won, "acquire at 1s of a 1500ms TTL")

	f.advance(600 * time.Millisecond)
	won, err = other.Acquire(ctx, time.Minute)
	require.NoError(t, err)
	assert.True(t, won, "acquire after expiry")
}

func TestClaimOnceHasOneWinnerPerTTL(t *testing.T) {
	f := newFakeValkey(t)
	ctx := context.Background()
	claim := func() bool {
		won, err := ClaimOnce(ctx, f.client, "test:claim", 30*time.Second)
		require.NoError(t, err)
		return won
	}

	first, second := claim(), claim()
	f.advance(30 * time.Second)
	afterExpiry := claim()

	assert.Equal(t, []bool{true, false, true}, []bool{first, second, afterExpiry})
}

func TestRecordRecentThenCountSinceSeesOnlyLinesInsideTheWindow(t *testing.T) {
	f := newFakeValkey(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	log := RecentLog{Key: "k", Keep: 100, TTL: time.Hour}

	for _, age := range []time.Duration{10 * time.Minute, 2 * time.Minute, 0} {
		require.NoError(t, RecordRecent(ctx, f.client, log, now.Add(-age)))
	}
	n, err := CountRecentSince(ctx, f.client, "k", now.Add(-5*time.Minute))

	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "a line older than the window must not count")
}

func TestCountRecentSinceMissingKeyReadsAsZero(t *testing.T) {
	f := newFakeValkey(t)

	n, err := CountRecentSince(context.Background(), f.client, "absent", time.Unix(0, 0))

	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestRecordRecentKeepsSameMillisecondLinesDistinct(t *testing.T) {
	f := newFakeValkey(t)
	now := time.Unix(1_700_000_000, 0)

	for range 3 {
		require.NoError(t, RecordRecent(context.Background(), f.client, RecentLog{Key: "k", Keep: 100, TTL: time.Hour}, now))
	}

	members := map[string]struct{}{}
	for _, entry := range f.zset("k") {
		members[entry.member] = struct{}{}
	}
	assert.Len(t, members, 3)
}

func TestRecordRecentTrimsToNewestKeepAndSetsTTL(t *testing.T) {
	f := newFakeValkey(t)
	now := time.Unix(1_700_000_000, 0)
	log := RecentLog{Key: "k", Keep: 100, TTL: time.Hour}

	for i := range 130 {
		require.NoError(t, RecordRecent(context.Background(), f.client, log, now.Add(time.Duration(i)*time.Second)))
	}

	kept := f.zset("k")
	require.Len(t, kept, 100)
	assert.EqualValues(t, now.Add(30*time.Second).UnixMilli(), kept[0].score, "trim must drop the oldest entries")
	assert.EqualValues(t, time.Hour.Milliseconds(), f.pexpire("k"))
}
