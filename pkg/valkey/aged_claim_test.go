// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestAgedClaimHonorsGuardAgeAndDone(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	claim := AgedClaim{Key: "test:aged:" + testUniqueKey(t), Guard: "state", Want: "ready", Field: "claimed", DoneValue: "done", MaxAge: time.Minute}
	now := time.Now()

	won, err := claim.Claim(ctx, client, now)
	require.NoError(t, err)
	require.False(t, won, "guard not met")

	require.NoError(t, HashField{Key: claim.Key, Field: "state"}.Set(ctx, client, "ready"))
	won, err = claim.Claim(ctx, client, now)
	require.NoError(t, err)
	require.True(t, won)

	won, err = claim.Claim(ctx, client, now.Add(time.Second))
	require.NoError(t, err)
	require.False(t, won, "fresh claim holds")

	won, err = claim.Claim(ctx, client, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, won, "old claim can be retaken")

	require.NoError(t, claim.Done(ctx, client))
	won, err = claim.Claim(ctx, client, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, won, "done is final")
}
