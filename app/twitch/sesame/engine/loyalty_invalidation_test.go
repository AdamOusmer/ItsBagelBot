// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

const invalidationTestPrefix = "test.invalidate"

func TestLoyaltyInvalidationDropsChannelBalancesAndTop(t *testing.T) {
	ctx := t.Context()
	nc := testnats.Connect(t)
	served := bustest.Respond(t, nc, counterRPCPrefix+".top.get", `{"top":[{"viewer_id":"7","points":5}]}`)
	client := newHotPathTestClient(t)
	store := NewValkeyLoyaltyStore(client, NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
	store.StartInvalidationListener(nc, invalidationTestPrefix)
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

	require.NoError(t, invalidate.Publish(nc, invalidationTestPrefix, loyaltyrpc.InvalidateScope, strconv.FormatUint(channel, 10)))

	assert.Eventually(t, func() bool {
		return keyGone(t, client, balanceKey(channel, 7)) && keyGone(t, client, balanceKey(channel, 8))
	}, 3*time.Second, 10*time.Millisecond)
	assert.False(t, keyGone(t, client, balanceKey(other, 7)), "another channel keeps its cached balance")

	_, err := store.Top(ctx, channel, 10)
	require.NoError(t, err)
	_, err = store.Top(ctx, other, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, served.Load(), "only the reset channel reloads its top")
}

func keyGone(t *testing.T, client valkey.Client, key string) bool {
	t.Helper()
	_, err := client.Do(t.Context(), client.B().Get().Key(key).Build()).ToString()
	return valkey.IsValkeyNil(err)
}
