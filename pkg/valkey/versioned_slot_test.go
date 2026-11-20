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

	checkVersionedSlot(t, client, slot, expectedSlot{"10", 10})

	applied, err = slot.Clear(ctx, client, 5, time.Minute)
	require.NoError(t, err)
	require.False(t, applied, "a lower version must not clear past a higher one")

	applied, err = slot.Clear(ctx, client, 20, time.Minute)
	require.NoError(t, err)
	require.True(t, applied)

	checkVersionedSlot(t, client, slot, expectedSlot{"", 20})
}

type expectedSlot struct {
	value   string
	version int64
}

func checkVersionedSlot(t *testing.T, client valkey_go.Client, slot VersionedSlot, expected expectedSlot) {
	t.Helper()
	value, version, err := slot.Read(t.Context(), client)
	require.NoError(t, err)
	require.Equal(t, expected, expectedSlot{value, version})
}
