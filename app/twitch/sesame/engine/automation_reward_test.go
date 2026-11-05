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
	"go.uber.org/zap"
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

func TestRewardStepsCarryTheirRunAndADistinctSlot(t *testing.T) {
	fx := newAutoFixture(manual("main",
		loopOf("2", act("reward_update", obj{"reward_id": "rw-1", "cost": 50})),
		act("reward_update", obj{"reward_id": "rw-2", "cost": 75}),
	))

	rec := fx.run(t, "main", "req-reward-slots", nil)

	sent := fx.messages(t)
	require.Len(t, sent, 3)
	var recorded []string
	for _, action := range rec.Actions {
		if action.Slot != "" {
			recorded = append(recorded, action.Slot)
		}
	}
	require.Len(t, recorded, 3)
	assert.Len(t, map[string]bool{recorded[0]: true, recorded[1]: true, recorded[2]: true}, 3, "a loop iteration is its own step")
	for i, msg := range sent {
		assert.Equal(t, rec.RunID, msg.RunID)
		assert.Equal(t, recorded[i], msg.ActionSlot)
	}
}

func queuedRewardRecord(runID string, slots ...string) automationrpc.RunRecord {
	rec := automationrpc.RunRecord{RunID: runID, AutomationID: "a", Outcome: automationrpc.OutcomeOK}
	for _, slot := range slots {
		rec.Actions = append(rec.Actions, automationrpc.ActionRecord{Type: "reward_update", Status: automationrpc.StatusOK, Detail: rewardQueued, Slot: slot})
	}
	return rec
}

func TestRecentOverlaysRewardResultsOnRuns(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	runs := NewValkeyRunLog(client, zap.NewNop())
	t.Cleanup(runs.Close)
	bid := uniqueBroadcaster()
	id := strconv.FormatUint(bid, 10)
	write := func(runID, slot, result string) {
		key := outgress.RewardResultKey(id, runID)
		require.NoError(t, client.Do(ctx, client.B().Hset().Key(key).FieldValue().FieldValue(slot, result).Build()).Error())
		t.Cleanup(func() { _ = client.Do(ctx, client.B().Del().Key(key).Build()).Error() })
	}

	worked := queuedRewardRecord("worked", "s1")
	refused := queuedRewardRecord("refused", "s1", "s2")
	silent := queuedRewardRecord("silent", "s1")
	alreadyPartial := queuedRewardRecord("partial", "s1")
	alreadyPartial.Outcome = automationrpc.OutcomePartial
	for _, rec := range []automationrpc.RunRecord{worked, refused, silent, alreadyPartial} {
		runs.Record(ctx, bid, rec)
	}
	write("worked", "s1", outgress.RewardResultOK)
	write("refused", "s1", outgress.RewardResultOK)
	write("refused", "s2", outgress.RewardResultNotManageable)
	write("partial", "s1", outgress.RewardResultNotFound)

	got, err := runs.Recent(ctx, bid, "", 0)
	require.NoError(t, err)
	require.Len(t, got, 4)
	byID := map[string]automationrpc.RunRecord{}
	for _, rec := range got {
		byID[rec.RunID] = rec
		for _, action := range rec.Actions {
			assert.Empty(t, action.Slot, "slots stay off the wire")
		}
	}

	assert.Equal(t, automationrpc.OutcomeOK, byID["worked"].Outcome)
	assert.Equal(t, []string{"0:reward_update:ok:"}, statuses(byID["worked"]))

	assert.Equal(t, automationrpc.OutcomePartial, byID["refused"].Outcome, "an ok run with a failed reward reads partial")
	assert.Equal(t, []string{"0:reward_update:ok:", "0:reward_update:failed:reward_not_manageable"}, statuses(byID["refused"]))

	assert.Equal(t, automationrpc.OutcomeOK, byID["silent"].Outcome)
	assert.Equal(t, []string{"0:reward_update:ok:queued"}, statuses(byID["silent"]), "no answer yet keeps the queued wording")

	assert.Equal(t, automationrpc.OutcomePartial, byID["partial"].Outcome)
	assert.Equal(t, []string{"0:reward_update:failed:reward_not_found"}, statuses(byID["partial"]))
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
