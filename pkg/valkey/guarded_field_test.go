// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestGuardedFieldWriteOnlyAppliesWhileTheGuardMatches(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	key := "test:guardedfield:" + t.Name()
	write := GuardedFieldWrite{Key: key, GuardField: "generation", Field: "display_name"}

	require.NoError(t, client.Do(ctx, client.B().Hset().Key(key).FieldValue().FieldValue("generation", "3").Build()).Error())

	applied, err := write.Apply(ctx, client, "2", "stale")
	require.NoError(t, err)
	require.False(t, applied)

	applied, err = write.Apply(ctx, client, "3", "fresh")
	require.NoError(t, err)
	require.True(t, applied)

	value, err := client.Do(ctx, client.B().Hget().Key(key).Field("display_name").Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, "fresh", value)
}
