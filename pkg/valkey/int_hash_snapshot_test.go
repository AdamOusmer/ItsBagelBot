// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/stretchr/testify/require"
)

func TestIntHashSnapshotReplacesFieldsAndExpires(t *testing.T) {
	client := valkeytest.Spawn(t)
	snapshot := pkgvalkey.IntHashSnapshot{Key: "snapshot", ExpireAt: time.Now().Add(time.Hour).Truncate(time.Second)}
	for _, values := range []map[string]int64{{"stale": 1}, {"current": 2, "zero": 0}, {}} {
		snapshot.Values = values
		require.NoError(t, snapshot.Replace(t.Context(), client))
		got, err := pkgvalkey.HashAllInts(t.Context(), client, snapshot.Key)
		require.NoError(t, err)
		require.Equal(t, values, got)
	}
	snapshot.Values = map[string]int64{"current": 3}
	require.NoError(t, snapshot.Replace(t.Context(), client))
	expires, err := client.Do(t.Context(), client.B().Expiretime().Key(snapshot.Key).Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, snapshot.ExpireAt.Unix(), expires)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, snapshot.Replace(ctx, client))
}
