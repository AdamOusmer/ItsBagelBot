// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestGuardedTransitionSkipsProtectedValuesAndTogglesMembership(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	key := "test:guarded:" + t.Name()
	index := "test:guarded:index:" + t.Name()
	flagged := "test:guarded:flagged:" + t.Name()
	transition := GuardedTransition{Key: key, Index: index, Flagged: flagged}

	require.NoError(t, client.Do(ctx, client.B().Hset().Key(key).FieldValue().FieldValue("state", "pending").Build()).Error())

	applied, err := transition.Apply(ctx, client, "state",
		[]FieldValue{{Field: "state", Value: "pending"}}, "member-1", false, "pending")
	require.NoError(t, err)
	require.False(t, applied, "the current value is protected, so nothing should change")

	applied, err = transition.Apply(ctx, client, "state",
		[]FieldValue{{Field: "state", Value: "failing"}, {Field: "error", Value: "boom"}}, "member-1", true, "receiving")
	require.NoError(t, err)
	require.True(t, applied)

	state, err := client.Do(ctx, client.B().Hget().Key(key).Field("state").Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, "failing", state)

	errMsg, err := client.Do(ctx, client.B().Hget().Key(key).Field("error").Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, "boom", errMsg)

	inIndex, err := client.Do(ctx, client.B().Sismember().Key(index).Member("member-1").Build()).AsBool()
	require.NoError(t, err)
	require.True(t, inIndex)

	inFlagged, err := client.Do(ctx, client.B().Sismember().Key(flagged).Member("member-1").Build()).AsBool()
	require.NoError(t, err)
	require.True(t, inFlagged)

	applied, err = transition.Apply(ctx, client, "state",
		[]FieldValue{{Field: "state", Value: "ok"}}, "member-1", false, "failing")
	require.NoError(t, err)
	require.False(t, applied, "the now-protected value must block the write")
}
