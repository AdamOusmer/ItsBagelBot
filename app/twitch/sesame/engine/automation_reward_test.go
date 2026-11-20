// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const recipeRedemption = `{"id":"red-1","broadcaster_user_id":"123","user_id":"7","user_login":"bob","user_name":"Bob","reward":{"id":"r-1","title":"Hydrate","cost":100}}`

func TestProcessRewardRedemptionOutcome(t *testing.T) {
	cases := []struct {
		name       string
		onRedeem   string
		rewardID   string
		permission string
		queueFull  bool
		want       []string
	}{
		{"fulfill", "fulfill", "r-1", "everyone", false, []string{outgress.RedemptionFulfilled}},
		{"cancel", "cancel", "r-1", "everyone", false, []string{outgress.RedemptionCanceled}},
		{"leave", "leave", "r-1", "everyone", false, nil},
		{"unset", "", "r-1", "everyone", false, nil},
		{"other reward", "fulfill", "r-2", "everyone", false, nil},
		{"condition fails with fulfill", "fulfill", "r-1", "mod", false, []string{outgress.RedemptionCanceled}},
		{"condition fails with leave", "leave", "r-1", "mod", false, []string{outgress.RedemptionCanceled}},
		{"condition fails with unset", "", "r-1", "mod", false, []string{outgress.RedemptionCanceled}},
		{"queue refuses the run", "fulfill", "r-1", "everyone", true, []string{outgress.RedemptionCanceled}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testAutomation("t", "reward", obj{"reward_id": tc.rewardID, "on_redeem": tc.onRedeem}, cond("perm", obj{"level": tc.permission}))
			a.Definition.Queue = queueName
			pub := &fakePublisher{}
			p, _ := autoRig{automations: []automation.Automation{a}, pub: pub}.pipeline()
			t.Cleanup(p.Close)
			p.automations.Queue = rewardTestQueue{full: tc.queueFull}
			process(t, p, eventEnv(RedemptionAddType, recipeRedemption))
			assert.Equal(t, tc.want, rewardResolutions(t, pub))
		})
	}
}

type rewardTestQueue struct {
	openRunQueue
	full   bool
	permit <-chan struct{}
}

func (q rewardTestQueue) Enter(context.Context, RunQueueSeat) (int64, error) {
	if q.waiting() {
		return 1, nil
	}
	if q.full {
		return RunQueueOut, nil
	}
	return 0, nil
}

func (q rewardTestQueue) Touch(ctx context.Context, seat RunQueueSeat) (int64, error) {
	return q.Enter(ctx, seat)
}

func (q rewardTestQueue) waiting() bool {
	if q.permit == nil {
		return false
	}
	select {
	case <-q.permit:
		return false
	default:
		return true
	}
}

func rewardResolutions(t *testing.T, pub *fakePublisher) []string {
	t.Helper()
	var got []string
	for _, m := range (&autoFixture{pub: pub}).messages(t) {
		if m.Type != outgress.TypeRedemptionUpdate {
			continue
		}
		assert.Equal(t, [2]string{"red-1", "r-1"}, [2]string{m.RedemptionID, m.RewardID})
		got = append(got, m.Status)
	}
	return got
}

func TestProcessRewardRedemptionWaitsForQueueDecision(t *testing.T) {
	for _, tc := range []struct {
		name string
		full bool
		want string
	}{
		{"granted", false, outgress.RedemptionFulfilled},
		{"refused", true, outgress.RedemptionCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testAutomation("t", "reward", obj{"reward_id": "r-1", "on_redeem": "fulfill"})
			a.Definition.Queue = queueName
			pub := &fakePublisher{}
			p, runs := autoRig{automations: []automation.Automation{a}, pub: pub}.pipeline()
			t.Cleanup(p.Close)
			permit := make(chan struct{})
			p.automations.Queue = rewardTestQueue{permit: permit, full: tc.full}
			p.queueTiming = queueTestTiming

			process(t, p, eventEnv(RedemptionAddType, recipeRedemption))
			assert.Empty(t, pub.snapshot())
			close(permit)
			require.Eventually(t, func() bool { return len(runs.summary()) > 0 }, time.Second, time.Millisecond)
			assert.Equal(t, []string{tc.want}, rewardResolutions(t, pub))
		})
	}
}

func TestRewardUpdateStepEmitsOutgressAction(t *testing.T) {
	fx := newAutoFixture(manual("main",
		act("reward_update", obj{"reward_id": "rw-1", "enabled": false, "cost": 50, "reset_on_stream_end": true}),
		act("reward_update", obj{"reward_id": "rw-2", "enabled": nil, "cost": 75, "reset_on_stream_end": false}),
		act("reward_update", obj{"reward_id": "rw-3", "enabled": true, "cost": nil}),
		act("reward_update", obj{"reward_id": "rw-4", "paused": true, "cooldown_s": 60}),
		act("reward_update", obj{"reward_id": "rw-5", "paused": false, "cooldown_s": 0}),
	))

	rec := fx.run(t, "main", "req-reward", nil)

	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, []string{
		`reward_update reward=rw-1 {"enabled":false,"cost":50,"reset_on_stream_end":true}`,
		`reward_update reward=rw-2 {"cost":75}`,
		`reward_update reward=rw-3 {"enabled":true}`,
		`reward_update reward=rw-4 {"paused":true,"global_cooldown_seconds":60}`,
		`reward_update reward=rw-5 {"paused":false,"global_cooldown_seconds":0}`,
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
		rec.Actions = append(rec.Actions, automationrpc.ActionRecord{Type: "reward_update", Status: automationrpc.StatusSent, Detail: rewardQueued, Slot: slot})
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
	assert.Equal(t, []string{"0:reward_update:sent:queued"}, statuses(byID["silent"]), "no answer yet keeps the queued wording")

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
