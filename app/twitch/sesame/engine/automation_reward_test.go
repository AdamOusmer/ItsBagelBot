// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"

	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewardUpdateStepEmitsOutgressAction(t *testing.T) {
	fx := newAutoFixture(manual("main",
		act("reward_update", obj{"reward_id": "rw-1", "enabled": false, "cost": 50, "reset_on_stream_end": true}),
		act("reward_update", obj{"reward_id": "rw-2", "enabled": nil, "cost": 75, "reset_on_stream_end": false}),
		act("reward_update", obj{"reward_id": "rw-3", "enabled": true, "cost": nil}),
	))

	rec := fx.run(t, "main", "req-reward", nil)

	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, []string{
		`reward_update reward=rw-1 {"enabled":false,"cost":50,"reset_on_stream_end":true}`,
		`reward_update reward=rw-2 {"cost":75}`,
		`reward_update reward=rw-3 {"enabled":true}`,
	}, fx.described(t))
}

func TestRewardRestorePendingFollowsSnapshotAndLiveVersion(t *testing.T) {
	client := newHotPathTestClient(t)
	ctx := context.Background()
	resets := NewValkeyRewardResets(client)
	const id uint64 = 5701
	snapshotKey := outgress.RewardSnapshotKey(strconv.FormatUint(id, 10))
	cleanup := func() { _ = client.Do(ctx, client.B().Del().Key(snapshotKey).Key(livekey.VerKey(id)).Build()).Error() }
	cleanup()
	t.Cleanup(cleanup)

	pending, err := resets.RestorePending(ctx, id, 100)
	require.NoError(t, err)
	assert.False(t, pending, "a stream that never changed a reward owes nothing")

	require.NoError(t, client.Do(ctx, client.B().Hset().Key(snapshotKey).FieldValue().FieldValue("rw-1", `{"enabled":true,"cost":1}`).Build()).Error())

	pending, err = resets.RestorePending(ctx, id, 100)
	require.NoError(t, err)
	assert.True(t, pending, "no applied version yet")

	require.NoError(t, client.Do(ctx, client.B().Set().Key(livekey.VerKey(id)).Value("100").Build()).Error())
	pending, err = resets.RestorePending(ctx, id, 100)
	require.NoError(t, err)
	assert.True(t, pending, "a redelivered offline at the applied version still counts")

	require.NoError(t, client.Do(ctx, client.B().Set().Key(livekey.VerKey(id)).Value("200").Build()).Error())
	pending, err = resets.RestorePending(ctx, id, 100)
	require.NoError(t, err)
	assert.False(t, pending, "an offline older than the running stream must wait")
}
