// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

func TestVersionedSlotRejectsAStaleVersionAndClearHonorsTheSameGuard(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	key := testUniqueKey(t)
	slot := VersionedSlot{Key: "test:slot:" + key, VerKey: "test:slot:ver:" + key}

	applied, err := slot.Set(ctx, client, SlotWrite{Version: 10, TTL: time.Minute, VerTTL: time.Minute})
	require.NoError(t, err)
	require.True(t, applied)

	applied, err = slot.Set(ctx, client, SlotWrite{Version: 5, TTL: time.Minute, VerTTL: time.Minute})
	require.NoError(t, err)
	require.False(t, applied, "a lower version must not overwrite a higher one")

	value, err := client.Do(ctx, client.B().Get().Key(slot.Key).Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, "10", value)

	applied, err = slot.Clear(ctx, client, 5, time.Minute)
	require.NoError(t, err)
	require.False(t, applied, "a lower version must not clear past a higher one")

	applied, err = slot.Clear(ctx, client, 20, time.Minute)
	require.NoError(t, err)
	require.True(t, applied)

	_, err = client.Do(ctx, client.B().Get().Key(slot.Key).Build()).ToString()
	require.True(t, valkey_go.IsValkeyNil(err))
}
