// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

// testUniqueKey avoids collisions with leftover state from a prior run against a persistent real Valkey.
func testUniqueKey(t *testing.T) string {
	return t.Name() + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func TestMarkedLeaseClaimsThenFinishesThenStaysDone(t *testing.T) {
	client := valkeytest.Real(t)
	ctx := context.Background()
	lease := MarkedLease{Key: "test:lease:" + testUniqueKey(t), Done: "done"}

	n, err := RunScripts(ctx, client, []ScriptCall{lease.ClaimCall("owner-a", time.Minute)})[0].ToInt64()
	require.NoError(t, err)
	require.Equal(t, LeaseWon, n)

	n, err = RunScripts(ctx, client, []ScriptCall{lease.ClaimCall("owner-b", time.Minute)})[0].ToInt64()
	require.NoError(t, err)
	require.Equal(t, LeaseHeld, n)

	n, err = RunScripts(ctx, client, []ScriptCall{lease.FinishCall("owner-a", time.Minute)})[0].ToInt64()
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	n, err = RunScripts(ctx, client, []ScriptCall{lease.ClaimCall("owner-c", time.Minute)})[0].ToInt64()
	require.NoError(t, err)
	require.Equal(t, LeaseDone, n)
}
