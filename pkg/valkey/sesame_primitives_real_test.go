// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

// cleanKeys deletes keys before and after the test so state from a previous run never leaks in.
func cleanKeys(t *testing.T, client valkey_go.Client, keys ...string) {
	t.Helper()
	require.NoError(t, DelKeys(context.Background(), client, keys...))
	t.Cleanup(func() { _ = DelKeys(context.Background(), client, keys...) })
}

func TestSeededCounterRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()

	key := SeededCounter{Key: "test:seeded:key:" + t.Name(), TTL: time.Minute}
	field := SeededCounter{Key: "test:seeded:field:" + t.Name(), Field: "viewer", TTL: time.Minute}
	cleanKeys(t, client, key.Key, field.Key)

	seeded, err := key.Bump(ctx, client, 2, func() int64 { return 40 })
	require.NoError(t, err)
	require.EqualValues(t, 42, seeded)

	warm, err := key.Bump(ctx, client, 3, func() int64 {
		t.Fatal("seed must not be called once warm")
		return 0
	})
	require.NoError(t, err)
	require.EqualValues(t, 45, warm)

	seededField, err := field.Bump(ctx, client, 1, func() int64 { return 9 })
	require.NoError(t, err)
	require.EqualValues(t, 10, seededField)
	warmField, err := field.Bump(ctx, client, 4, func() int64 {
		t.Fatal("seed must not be called once warm")
		return 0
	})
	require.NoError(t, err)
	require.EqualValues(t, 14, warmField)
}

func TestFreshMemberSetRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	set := FreshMemberSet{Key: "test:fresh:" + t.Name()}
	cleanKeys(t, client, set.Key)

	first, err := set.Add(ctx, client, "viewer", time.Minute)
	require.NoError(t, err)
	require.True(t, first)

	second, err := set.Add(ctx, client, "viewer", time.Minute)
	require.NoError(t, err)
	require.False(t, second)

	ttl, err := TTLSeconds(ctx, client, set.Key)
	require.NoError(t, err)
	require.Positive(t, ttl)
}

func TestOutcomeClaimRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	key := "test:outcome:" + t.Name()
	cleanKeys(t, client, key)

	drawnAt := time.Now().Add(-time.Minute)
	require.NoError(t, HSet(ctx, client, key, "result", `{"winners":[{"id":"w1"}],"drawn_at":`+
		strconv.FormatInt(drawnAt.UnixMilli(), 10)+`}`))

	claim := OutcomeClaim{Key: key}
	code, err := claim.Claim(ctx, client, "w1", time.Hour, time.Now())
	require.NoError(t, err)
	require.Equal(t, OutcomeClaimOK, code)

	again, err := claim.Claim(ctx, client, "w1", time.Hour, time.Now())
	require.NoError(t, err)
	require.Equal(t, OutcomeClaimAlready, again)

	none, err := claim.Claim(ctx, client, "nobody", time.Hour, time.Now())
	require.NoError(t, err)
	require.Equal(t, OutcomeClaimNone, none)
}

func TestFencedClaimRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	claim := FencedClaim{Claim: "test:fenced:claim:" + t.Name(), Fence: "test:fenced:fence:" + t.Name()}
	cleanKeys(t, client, claim.Claim, claim.Fence)

	won, err := claim.Arm(ctx, client, "gen-1", time.Minute)
	require.NoError(t, err)
	require.True(t, won)

	wonAgain, err := claim.Arm(ctx, client, "gen-1", time.Minute)
	require.NoError(t, err)
	require.False(t, wonAgain, "claim already held under the same fence")

	wonAfterFenceChange, err := claim.Arm(ctx, client, "gen-2", time.Minute)
	require.NoError(t, err)
	require.True(t, wonAfterFenceChange, "a fence change clears the stale claim")
}

func TestGatedJoinRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	gate := GatedJoin{
		GuardA: "test:gated:guardA:" + t.Name(), GuardB: "test:gated:guardB:" + t.Name(),
		Set: "test:gated:set:" + t.Name(), Labels: "test:gated:labels:" + t.Name(),
	}
	cleanKeys(t, client, gate.GuardA, gate.GuardB, gate.Set, gate.Labels)

	open, added, count, err := gate.Join(ctx, client, 1, "viewer-1", "login-1", time.Minute)
	require.NoError(t, err)
	require.False(t, open, "guards are missing")
	require.False(t, added)
	require.Zero(t, count)

	require.NoError(t, NewKV(client).Set(ctx, Key{Name: gate.GuardA, TTL: time.Minute}, "1"))
	require.NoError(t, NewKV(client).Set(ctx, Key{Name: gate.GuardB, TTL: time.Minute}, "1"))

	open, added, count, err = gate.Join(ctx, client, 1, "viewer-1", "login-1", time.Minute)
	require.NoError(t, err)
	require.True(t, open)
	require.True(t, added)
	require.EqualValues(t, 1, count)

	open, added, count, err = gate.Join(ctx, client, 2, "viewer-1", "login-1", time.Minute)
	require.NoError(t, err)
	require.True(t, open)
	require.False(t, added, "already joined")
	require.EqualValues(t, 1, count)
}

func TestCASDocumentRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	doc := CASDocument{Key: "test:cas:" + t.Name(), TTL: time.Minute}
	cleanKeys(t, client, doc.Key)

	raw, err := doc.Read(ctx, client)
	require.NoError(t, err)
	require.Empty(t, raw)

	swapped, err := doc.Swap(ctx, client, "", `{"v":1}`)
	require.NoError(t, err)
	require.True(t, swapped)

	stale, err := doc.Swap(ctx, client, "", `{"v":2}`)
	require.NoError(t, err)
	require.False(t, stale, "expected value no longer matches")

	fresh, err := doc.Swap(ctx, client, `{"v":1}`, `{"v":2}`)
	require.NoError(t, err)
	require.True(t, fresh)

	raw, err = doc.Read(ctx, client)
	require.NoError(t, err)
	require.Equal(t, `{"v":2}`, raw)
}

func TestReceiptCounterRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	counter := ReceiptCounter{Counter: "test:receipt:counter:" + t.Name(), CounterTTL: time.Minute}
	r1, r2 := "test:receipt:r1:"+t.Name(), "test:receipt:r2:"+t.Name()
	cleanKeys(t, client, counter.Counter, r1, r2)

	today, total, err := counter.Bump(ctx, client, r1, 2*time.Minute, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, today)
	require.EqualValues(t, 100, total)

	replayToday, replayTotal, err := counter.Bump(ctx, client, r1, 2*time.Minute, 100)
	require.NoError(t, err)
	require.Equal(t, today, replayToday)
	require.Equal(t, total, replayTotal)

	nextToday, _, err := counter.Bump(ctx, client, r2, 2*time.Minute, 100)
	require.NoError(t, err)
	require.EqualValues(t, 2, nextToday)
}

func TestRepeatSequenceRealValkey(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	seq := RepeatSequence{Key: "test:repeat:" + t.Name()}
	cleanKeys(t, client, seq.Key)
	window := 15 * time.Second

	steps := []struct {
		width, copies int
		replay        string
	}{
		{1, 1, "m1"}, {2, 1, "m2"}, {3, 1, "m3"}, {2, 1, "m4"}, {1, 1, "m5"},
	}
	var flags, apex int64
	for _, s := range steps {
		var err error
		flags, _, apex, err = seq.Track(ctx, client, "emote", s.width, s.copies, s.replay, window, window, time.Minute, "5,10")
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), flags&1, "ascend-then-descend run should have completed")
	require.EqualValues(t, 3, apex)

	// Replaying the last message must not re-trigger completion.
	flagsReplay, _, _, err := seq.Track(ctx, client, "emote", 1, 1, "m5", window, window, time.Minute, "5,10")
	require.NoError(t, err)
	require.Zero(t, flagsReplay)
}
