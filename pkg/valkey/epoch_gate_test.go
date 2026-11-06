// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

func TestEpochGateActivateAndReleaseGuardOnOwnerSessionAndState(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	owner := "test:epoch:owner:" + t.Name()
	session := "test:epoch:session:" + t.Name()
	record := "test:epoch:record:" + t.Name()
	gate := EpochGate{Owner: owner, Session: session, Record: record}

	require.NoError(t, client.Do(ctx, client.B().Set().Key(owner).Value("9:owner").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Set().Key(session).Value("9:session-1").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(record).FieldValue().FieldValue("generation", "3").FieldValue("state", "pending").Build()).Error())

	activated, err := gate.Activate(ctx, client, "8", "3", "sub-1", "session-1", "0")
	require.NoError(t, err)
	require.False(t, activated, "a stale epoch must not activate")

	activated, err = gate.Activate(ctx, client, "9", "3", "sub-1", "session-1", "0")
	require.NoError(t, err)
	require.True(t, activated)

	subID, err := client.Do(ctx, client.B().Hget().Key(record).Field("subscription_id").Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, "sub-1", subID)

	require.NoError(t, client.Do(ctx, client.B().Hset().Key(record).FieldValue().FieldValue("state", "stopping").Build()).Error())

	released, err := gate.Release(ctx, client, "9", "3", "other-sub")
	require.NoError(t, err)
	require.False(t, released, "releasing an unrelated subscription id must fail")

	released, err = gate.Release(ctx, client, "9", "3", "sub-1")
	require.NoError(t, err)
	require.True(t, released)

	subID, err = client.Do(ctx, client.B().Hget().Key(record).Field("subscription_id").Build()).ToString()
	require.True(t, valkey_go.IsValkeyNil(err))
	_ = subID
}
