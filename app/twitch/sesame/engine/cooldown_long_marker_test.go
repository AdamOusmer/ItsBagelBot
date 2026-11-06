// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type scriptedLongStore struct {
	expiry   int64
	refuse   bool
	claimErr error
	before   func()
	claims   int
	released []modulesrpc.CooldownWindow
}

func (s *scriptedLongStore) ClaimLongCooldowns(_ context.Context, claim LongCooldownClaim) (LongCooldownHold, bool, error) {
	s.claims++
	if s.before != nil {
		s.before()
	}
	if s.claimErr != nil {
		return LongCooldownHold{}, false, s.claimErr
	}
	hold := LongCooldownHold{BroadcasterID: claim.BroadcasterID}
	for _, window := range claim.Windows {
		hold.Windows = append(hold.Windows, modulesrpc.CooldownWindow{Key: window.Key, ExpiresAtMS: s.expiry})
	}
	return hold, !s.refuse, nil
}

func (s *scriptedLongStore) ReleaseLongCooldowns(_ context.Context, hold LongCooldownHold) error {
	s.released = append(s.released, hold.Windows...)
	return nil
}

func markerRig(t *testing.T) (valkey.Client, uint64) {
	t.Helper()
	return newHotPathTestClient(t), uint64(time.Now().UnixNano())
}

func longWindow(key string) []CooldownWindow {
	return []CooldownWindow{{Key: key, TTL: 2 * 24 * time.Hour}}
}

func markerTTL(t *testing.T, client valkey.Client, channel uint64, key string) time.Duration {
	t.Helper()
	ms, err := client.Do(context.Background(), client.B().Pttl().Key(automation.LongCooldownMarkerKey(channel, key)).Build()).AsInt64()
	require.NoError(t, err)
	return time.Duration(ms) * time.Millisecond
}

func setMarker(t *testing.T, client valkey.Client, key string, expiry int64) {
	t.Helper()
	cmd := client.B().Set().Key(key).Value(automation.LongCooldownMarkerValue(expiry)).PxMilliseconds(time.Hour.Milliseconds()).Build()
	require.NoError(t, client.Do(context.Background(), cmd).Error())
}

func TestMarkerRefusesWithoutAskingTheStore(t *testing.T) {
	client, channel := markerRig(t)
	store := &scriptedLongStore{expiry: time.Now().Add(48 * time.Hour).UnixMilli()}
	marked := NewMarkedLongCooldowns(client, store, zap.NewNop())
	ctx := context.Background()

	_, won, err := marked.ClaimLongCooldowns(ctx, LongCooldownClaim{BroadcasterID: channel, Windows: longWindow("k")})
	require.NoError(t, err)
	require.True(t, won)
	assert.InDelta(t, float64(24*time.Hour), float64(markerTTL(t, client, channel, "k")), float64(time.Minute), "a long window marks for a day at most")

	_, won, err = marked.ClaimLongCooldowns(ctx, LongCooldownClaim{BroadcasterID: channel, Windows: longWindow("k")})
	require.NoError(t, err)
	assert.False(t, won)
	assert.Equal(t, 1, store.claims, "the marker answered the second attempt")
}

func TestRefusalRewritesTheMarkerFromTheRepliedExpiry(t *testing.T) {
	client, channel := markerRig(t)
	store := &scriptedLongStore{expiry: time.Now().Add(time.Hour).UnixMilli(), refuse: true}
	marked := NewMarkedLongCooldowns(client, store, zap.NewNop())

	_, won, err := marked.ClaimLongCooldowns(context.Background(), LongCooldownClaim{BroadcasterID: channel, Windows: longWindow("k")})

	require.NoError(t, err)
	assert.False(t, won)
	assert.InDelta(t, float64(time.Hour), float64(markerTTL(t, client, channel, "k")), float64(time.Minute))
	assert.Empty(t, store.released)
}

func TestReleaseClearsOnlyItsOwnMarker(t *testing.T) {
	client, channel := markerRig(t)
	ctx := context.Background()
	store := &scriptedLongStore{expiry: time.Now().Add(48 * time.Hour).UnixMilli()}
	marked := NewMarkedLongCooldowns(client, store, zap.NewNop())

	hold, won, err := marked.ClaimLongCooldowns(ctx, LongCooldownClaim{BroadcasterID: channel, Windows: longWindow("mine")})
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, marked.ReleaseLongCooldowns(ctx, hold))
	assert.Equal(t, time.Duration(-2)*time.Millisecond, markerTTL(t, client, channel, "mine"), "the released window is free again")

	setMarker(t, client, automation.LongCooldownMarkerKey(channel, "later"), store.expiry+1)
	require.NoError(t, marked.ReleaseLongCooldowns(ctx, LongCooldownHold{BroadcasterID: channel, Windows: []modulesrpc.CooldownWindow{{Key: "later", ExpiresAtMS: store.expiry}}}))
	assert.Positive(t, markerTTL(t, client, channel, "later"), "a later holder keeps its marker")
}

func TestMoveDuringTheClaimRefusesAndGivesTheRowBack(t *testing.T) {
	client, channel := markerRig(t)
	moved := time.Now().Add(20 * time.Hour).UnixMilli()
	store := &scriptedLongStore{expiry: time.Now().Add(48 * time.Hour).UnixMilli()}
	store.before = func() { setMarker(t, client, automation.LongCooldownMarkerKey(channel, "k"), moved) }
	marked := NewMarkedLongCooldowns(client, store, zap.NewNop())

	_, won, err := marked.ClaimLongCooldowns(context.Background(), LongCooldownClaim{BroadcasterID: channel, Windows: longWindow("k")})

	require.NoError(t, err)
	assert.False(t, won, "the moved window is still running")
	require.Len(t, store.released, 1, "the row this claim took goes back")
	got, err := client.Do(context.Background(), client.B().Get().Key(automation.LongCooldownMarkerKey(channel, "k")).Build()).ToString()
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(moved, 10), got, "the moved marker stays")
}

func TestConcurrentRefusalWritingTheSameExpiryIsNotALoss(t *testing.T) {
	client, channel := markerRig(t)
	expiry := time.Now().Add(48 * time.Hour).UnixMilli()
	store := &scriptedLongStore{expiry: expiry}
	store.before = func() { setMarker(t, client, automation.LongCooldownMarkerKey(channel, "k"), expiry) }
	marked := NewMarkedLongCooldowns(client, store, zap.NewNop())

	_, won, err := marked.ClaimLongCooldowns(context.Background(), LongCooldownClaim{BroadcasterID: channel, Windows: longWindow("k")})

	require.NoError(t, err)
	assert.True(t, won)
	assert.Empty(t, store.released)
}

func TestValkeyDownGoesToTheDatabase(t *testing.T) {
	server := valkeytest.New(t)
	down := func(valkeytest.Args) bool { return true }
	for _, cmd := range []string{"EXISTS", "SET", "EVALSHA", "EVAL"} {
		server.Fail(valkeytest.Failure{Cmd: cmd, Match: down, Message: "ERR down"})
	}
	ctx := context.Background()
	claim := LongCooldownClaim{BroadcasterID: 7, Windows: longWindow("k")}

	store := &scriptedLongStore{expiry: time.Now().Add(48 * time.Hour).UnixMilli()}
	marked := NewMarkedLongCooldowns(server.Client(), store, zap.NewNop())
	hold, won, err := marked.ClaimLongCooldowns(ctx, claim)
	require.NoError(t, err)
	assert.True(t, won, "the database won the window")
	assert.Equal(t, 1, store.claims)
	require.NoError(t, marked.ReleaseLongCooldowns(ctx, hold))
	assert.Len(t, store.released, 1)

	refusing := NewMarkedLongCooldowns(server.Client(), &scriptedLongStore{expiry: store.expiry, refuse: true}, zap.NewNop())
	_, won, err = refusing.ClaimLongCooldowns(ctx, claim)
	require.NoError(t, err)
	assert.False(t, won, "the database refusal stands")

	broken := NewMarkedLongCooldowns(server.Client(), &scriptedLongStore{claimErr: errors.New("db down")}, zap.NewNop())
	_, won, err = broken.ClaimLongCooldowns(ctx, claim)
	assert.Error(t, err)
	assert.False(t, won, "both stores down fails closed")
}
