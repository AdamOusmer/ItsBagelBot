// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestVersionedFlagSetIncrementsVersionAndReadMatches(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	key := testUniqueKey(t)
	flag := VersionedFlag{Flag: "test:flag:" + key, Version: "test:flag:ver:" + key}

	paused, version, err := flag.Read(ctx, client)
	require.NoError(t, err)
	require.False(t, paused)
	require.Zero(t, version)

	version, err = flag.Set(ctx, client, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), version)

	paused, readVersion, err := flag.Read(ctx, client)
	require.NoError(t, err)
	require.True(t, paused)
	require.Equal(t, int64(1), readVersion)

	version, err = flag.Set(ctx, client, false)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)

	paused, readVersion, err = flag.Read(ctx, client)
	require.NoError(t, err)
	require.False(t, paused)
	require.Equal(t, int64(2), readVersion)
}
