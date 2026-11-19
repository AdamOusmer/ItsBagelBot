// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

const invalidationTestPrefix = "test.invalidate"

func TestLoyaltyInvalidationDropsTopWithoutRepeatingSharedDeletion(t *testing.T) {
	ctx := t.Context()
	nc := testnats.Connect(t)
	served := bustest.Respond(t, nc, counterRPCPrefix+".top.get", `{"top":[{"viewer_id":"7","points":5}]}`)
	client := newHotPathTestClient(t)
	store := NewValkeyLoyaltyStore(client, NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
	h := bus.NewInvalidations(nc, invalidationTestPrefix, nil)
	store.BindInvalidations(h)
	t.Cleanup(store.Close)

	channel, other := freshChannel(), freshChannel()
	for _, key := range []string{balanceKey(channel, 7), balanceKey(channel, 8), balanceKey(other, 7)} {
		require.NoError(t, client.Do(ctx, client.B().Set().Key(key).Value("5:0").ExSeconds(60).Build()).Error())
	}
	for _, id := range []uint64{channel, other} {
		_, err := store.Top(ctx, id, 10)
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, served.Load())

	h.Publish(invalidate.ScopeLoyalty, channel)

	assert.False(t, keyGone(t, client, balanceKey(channel, 7)))
	assert.False(t, keyGone(t, client, balanceKey(channel, 8)))
	assert.False(t, keyGone(t, client, balanceKey(other, 7)), "another channel keeps its cached balance")

	_, err := store.Top(ctx, channel, 10)
	require.NoError(t, err)
	_, err = store.Top(ctx, other, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, served.Load(), "only the reset channel reloads its top")
}

func TestLoyaltyKeyedInvalidationDoesNotRepeatSharedDeletion(t *testing.T) {
	ctx := t.Context()
	nc := testnats.Connect(t)
	client := newHotPathTestClient(t)
	store := NewValkeyLoyaltyStore(client, NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
	h := bus.NewInvalidations(nc, invalidationTestPrefix, nil)
	store.BindInvalidations(h)
	t.Cleanup(store.Close)

	channel := freshChannel()
	for _, key := range []string{balanceKey(channel, 7), balanceKey(channel, 8)} {
		require.NoError(t, client.Do(ctx, client.B().Set().Key(key).Value("5:0").ExSeconds(60).Build()).Error())
	}

	h.Publish(invalidate.ScopeLoyalty, channel, "7")

	assert.False(t, keyGone(t, client, balanceKey(channel, 7)))
	assert.False(t, keyGone(t, client, balanceKey(channel, 8)), "a viewer who earned nothing keeps the cached balance")
}

func keyGone(t *testing.T, client valkey.Client, key string) bool {
	t.Helper()
	_, err := client.Do(t.Context(), client.B().Get().Key(key).Build()).ToString()
	return valkey.IsValkeyNil(err)
}
