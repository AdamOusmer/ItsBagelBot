// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cooldownmarker_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/cooldownmarker"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func armMarker(channel uint64, expiresAtMS int64) cooldownmarker.Marker {
	return cooldownmarker.Marker{Channel: channel, Window: "subathon", ExpiresAtMS: expiresAtMS}
}

func TestArmRaisesAFreshMarkerAndIsMonotonic(t *testing.T) {
	client := valkeytest.Real(t)
	store := cooldownmarker.New(client)
	ctx := context.Background()
	channel := uint64(time.Now().UnixNano())
	now := time.Now()

	first := now.Add(10 * time.Hour).UnixMilli()
	armed, err := store.Arm(ctx, armMarker(channel, first))
	require.NoError(t, err)
	assert.True(t, armed, "no marker existed yet")

	key := automation.LongCooldownMarkerKey(channel, "subathon")
	value, err := client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(first, 10), value)

	earlier := now.Add(5 * time.Hour).UnixMilli()
	armed, err = store.Arm(ctx, armMarker(channel, earlier))
	require.NoError(t, err)
	assert.False(t, armed, "an out-of-order redelivery never moves the marker backward")

	value, err = client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(first, 10), value, "the earlier value was rejected, not overwritten")

	later := now.Add(20 * time.Hour).UnixMilli()
	armed, err = store.Arm(ctx, armMarker(channel, later))
	require.NoError(t, err)
	assert.True(t, armed, "a genuinely later expiry still arms")

	value, err = client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(later, 10), value)
}

func TestArmNeverResurrectsAnExpiredWindow(t *testing.T) {
	client := valkeytest.Real(t)
	store := cooldownmarker.New(client)
	ctx := context.Background()
	channel := uint64(time.Now().UnixNano())

	armed, err := store.Arm(ctx, armMarker(channel, time.Now().Add(-time.Minute).UnixMilli()))
	require.NoError(t, err)
	assert.False(t, armed)

	key := automation.LongCooldownMarkerKey(channel, "subathon")
	exists, err := client.Do(ctx, client.B().Exists().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	assert.Zero(t, exists)
}

func TestArmCapsThePhysicalTTLEvenForAFarExpiry(t *testing.T) {
	client := valkeytest.Real(t)
	store := cooldownmarker.New(client)
	ctx := context.Background()
	channel := uint64(time.Now().UnixNano())

	far := time.Now().Add(20 * 24 * time.Hour).UnixMilli()
	armed, err := store.Arm(ctx, armMarker(channel, far))
	require.NoError(t, err)
	require.True(t, armed)

	key := automation.LongCooldownMarkerKey(channel, "subathon")
	ttl, err := client.Do(ctx, client.B().Pttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	assert.LessOrEqual(t, time.Duration(ttl)*time.Millisecond, automation.LongCooldownMarkerMaxTTL+time.Minute)
	assert.Greater(t, time.Duration(ttl)*time.Millisecond, automation.LongCooldownMarkerMaxTTL-time.Minute)

	value, err := client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(far, 10), value, "the stored value still carries the true, uncapped expiry")
}

func TestArmListsTheWindowInTheChannelIndex(t *testing.T) {
	client := valkeytest.Real(t)
	store := cooldownmarker.New(client)
	ctx := context.Background()
	channel := uint64(time.Now().UnixNano())

	_, err := store.Arm(ctx, armMarker(channel, time.Now().Add(time.Hour).UnixMilli()))
	require.NoError(t, err)

	live, err := store.AnyLive(ctx, channel, []string{"subathon"})
	require.NoError(t, err)
	assert.True(t, live)

	require.NoError(t, store.Erase(ctx, channel))
	live, err = store.AnyLive(ctx, channel, []string{"subathon"})
	require.NoError(t, err)
	assert.False(t, live, "erasure finds the window through the index")
}
