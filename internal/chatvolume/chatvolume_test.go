// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package chatvolume

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// ---- pure read-side reconstruction: no Valkey needed ----

func TestBuildChatVolumeEmptyRingIsAllZero(t *testing.T) {
	cv := buildChatVolume(map[string]string{}, 1_000_000)
	require.Len(t, cv.Buckets, ringWidth)
	for _, b := range cv.Buckets {
		require.Equal(t, 0, b)
	}
	require.Empty(t, cv.CommandTicks)
	require.Equal(t, 0, cv.Now)
	require.Equal(t, 0, cv.Peak)
}

func TestBuildChatVolumeReadsCurrentLapOnly(t *testing.T) {
	now := int64(1_000_100) // anchor + 100
	slot := slotName(now)
	fields := map[string]string{
		"a":  "1000000",
		slot: "100:7:1", // current minute (delta 100), 7 messages, handled
	}
	cv := buildChatVolume(fields, now)
	require.Equal(t, 7, cv.Now)
	require.Contains(t, cv.CommandTicks, ringWidth-1)
	require.Equal(t, 7, cv.Peak)
}

func TestBuildChatVolumeStaleLapReadsAsZero(t *testing.T) {
	now := int64(2_000_200)
	slot := slotName(now)
	fields := map[string]string{
		"a":  "2000000",
		slot: "999:42:1", // wrong delta for this lap (should be 200)
	}
	cv := buildChatVolume(fields, now)
	require.Equal(t, 0, cv.Now)
	require.Empty(t, cv.CommandTicks)
}

func TestBuildChatVolumeMissingAnchorIsAllZero(t *testing.T) {
	slot := slotName(500)
	cv := buildChatVolume(map[string]string{slot: "0:9:1"}, 500)
	// anchor parses to 0, so delta-for-target(500-0=500) must equal the
	// stored delta(0) to count -- it does not, so this reads as zero too.
	require.Equal(t, 0, cv.Now)
}

func TestParseSlotValueRejectsMalformed(t *testing.T) {
	cases := []string{"", "1:2", "a:2:0", "1:b:0", "1:2:0:extra-is-fine-since-splitn3"}
	for _, raw := range cases {
		_, _, _, ok := parseSlotValue(raw)
		if raw == "1:2:0:extra-is-fine-since-splitn3" {
			require.True(t, ok, raw) // SplitN(3) folds the rest into the 3rd field
			continue
		}
		require.False(t, ok, raw)
	}
}

func TestPeakOf(t *testing.T) {
	require.Equal(t, 9, peakOf([]int{0, 3, 9, 1}))
	require.Equal(t, 0, peakOf(nil))
}

// ---- integration: real Lua semantics need a real Valkey ----
//
// Opt-in like app/twitch/sesame/engine's hot-path tests (same VALKEY_TEST_ADDR
// convention): the reset-vs-increment decision lives in bumpScript, a Lua
// state machine that only exists inside a real Valkey interpreter.

func newChatVolumeTestClient(t *testing.T) valkey.Client {
	t.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{address},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func TestStoreBumpAccumulatesWithinOneMinute(t *testing.T) {
	client := newChatVolumeTestClient(t)
	ctx := context.Background()
	s := New(client, zap.NewNop())
	broadcaster := uint64(time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Do(ctx, client.B().Del().Key(chatVolKey(broadcaster)).Build()).Error() })

	minute := time.Unix(1_800_000*60, 0).UTC()
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: minute, Handled: false})
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: minute.Add(10 * time.Second), Handled: false})
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: minute.Add(20 * time.Second), Handled: true})

	cv, err := s.Read(ctx, broadcaster, minute.Add(30*time.Second))
	require.NoError(t, err)
	require.Equal(t, 3, cv.Now)
	require.Contains(t, cv.CommandTicks, ringWidth-1)
}

func TestStoreBumpResetsOnMinuteRollover(t *testing.T) {
	client := newChatVolumeTestClient(t)
	ctx := context.Background()
	s := New(client, zap.NewNop())
	broadcaster := uint64(time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Do(ctx, client.B().Del().Key(chatVolKey(broadcaster)).Build()).Error() })

	minute := time.Unix(1_800_100*60, 0).UTC()
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: minute, Handled: false})
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: minute, Handled: false})
	next := minute.Add(time.Minute)
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: next, Handled: false})

	cv, err := s.Read(ctx, broadcaster, next)
	require.NoError(t, err)
	require.Equal(t, 1, cv.Now, "new minute must reset the bucket, not add onto the previous minute's count")
	require.Equal(t, 2, cv.Buckets[ringWidth-2], "the previous minute's own bucket is untouched")
}

func TestStoreRingCollisionAfterFullLapReadsFresh(t *testing.T) {
	client := newChatVolumeTestClient(t)
	ctx := context.Background()
	s := New(client, zap.NewNop())
	broadcaster := uint64(time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Do(ctx, client.B().Del().Key(chatVolKey(broadcaster)).Build()).Error() })

	base := time.Unix(1_800_300*60, 0).UTC()
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: base, Handled: false})
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: base, Handled: false})
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: base, Handled: false})

	// A full lap later, same ring slot: must read as a fresh minute (count 1),
	// not 3+1=4 leaking across the wraparound.
	lapLater := base.Add(time.Duration(ringWidth) * time.Minute)
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: lapLater, Handled: false})

	cv, err := s.Read(ctx, broadcaster, lapLater)
	require.NoError(t, err)
	require.Equal(t, 1, cv.Now)
}

func TestStoreStreamOnlineClearsRing(t *testing.T) {
	client := newChatVolumeTestClient(t)
	ctx := context.Background()
	s := New(client, zap.NewNop())
	broadcaster := uint64(time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Do(ctx, client.B().Del().Key(chatVolKey(broadcaster)).Build()).Error() })

	now := time.Unix(1_800_500*60, 0).UTC()
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeChatMessage, At: now, Handled: false})
	s.Observe(Event{BroadcasterID: broadcaster, Type: typeStreamOnline, At: now, Handled: false})

	cv, err := s.Read(ctx, broadcaster, now)
	require.NoError(t, err)
	require.Equal(t, 0, cv.Now)
	require.Equal(t, 0, cv.Peak)
}

func TestStoreObserveIgnoresOtherEventTypes(t *testing.T) {
	client := newChatVolumeTestClient(t)
	ctx := context.Background()
	s := New(client, zap.NewNop())
	broadcaster := uint64(time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Do(ctx, client.B().Del().Key(chatVolKey(broadcaster)).Build()).Error() })

	now := time.Unix(1_800_600*60, 0).UTC()
	s.Observe(Event{BroadcasterID: broadcaster, Type: "channel.follow", At: now, Handled: false})

	// No key was ever created.
	exists, err := client.Do(ctx, client.B().Exists().Key(chatVolKey(broadcaster)).Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(0), exists)
}
