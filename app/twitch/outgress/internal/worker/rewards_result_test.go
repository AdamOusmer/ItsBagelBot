// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

const (
	testRunID = "run-1"
	testSlot  = "slot-a"
)

func trackedRewardBody(rewardID, slot string) string {
	return `{"type":"reward_update","broadcaster_id":"` + testBroadcaster + `","reward_id":"` + rewardID +
		`","run_id":"` + testRunID + `","action_slot":"` + slot + `","payload":{"cost":50}}`
}

func deliver(w *Worker, body string, final bool) error {
	msg := bus.NewMessage("reward-result", []byte(body))
	if final {
		msg.MarkFinalDelivery()
	}
	return w.Process(msg)
}

func rewardResults(t *testing.T, vc valkey.Client) map[string]string {
	t.Helper()
	key := outgress.RewardResultKey(testBroadcaster, testRunID)
	results, err := vc.Do(context.Background(), vc.B().Hgetall().Key(key).Build()).AsStrMap()
	require.NoError(t, err)
	return results
}

func TestRewardSuccessRecordsOKForTheRunLogsLifetime(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)

	require.NoError(t, deliver(w, trackedRewardBody("rw-1", testSlot), false))

	assert.Equal(t, map[string]string{testSlot: outgress.RewardResultOK}, rewardResults(t, vc))
	ttl, err := vc.Do(context.Background(), vc.B().Ttl().Key(outgress.RewardResultKey(testBroadcaster, testRunID)).Build()).AsInt64()
	require.NoError(t, err)
	assert.InDelta(t, outgress.RewardResultTTL.Seconds(), float64(ttl), 5)
}

func TestRewardRefusalRecordsItsReasonWithoutARetry(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   string
	}{
		{"not ours", http.StatusForbidden, outgress.RewardResultNotManageable},
		{"gone", http.StatusNotFound, outgress.RewardResultNotFound},
		{"rejected", http.StatusBadRequest, outgress.RewardResultRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
			w := rewardsWorker(t, api, vc)
			api.setStatus("rw-1", tc.status)

			require.NoError(t, deliver(w, trackedRewardBody("rw-1", testSlot), false))

			assert.Equal(t, map[string]string{testSlot: tc.want}, rewardResults(t, vc))
		})
	}
}

func TestRewardRetriesRecordNothingUntilTheLastDelivery(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	api.setStatus("rw-1", http.StatusBadGateway)

	require.Error(t, deliver(w, trackedRewardBody("rw-1", testSlot), false))
	assert.Empty(t, rewardResults(t, vc), "another delivery is coming")

	require.Error(t, deliver(w, trackedRewardBody("rw-1", testSlot), true))
	assert.Equal(t, map[string]string{testSlot: outgress.RewardResultUnconfirmed}, rewardResults(t, vc))
}

func TestRewardRetryThatWorksRecordsOK(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	api.setStatus("rw-1", http.StatusBadGateway)
	require.Error(t, deliver(w, trackedRewardBody("rw-1", testSlot), false))

	api.setStatus("rw-1", 0)
	require.NoError(t, deliver(w, trackedRewardBody("rw-1", testSlot), true))

	assert.Equal(t, map[string]string{testSlot: outgress.RewardResultOK}, rewardResults(t, vc))
}

func TestRedeliveryCannotTurnASuccessIntoAFailure(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	require.NoError(t, deliver(w, trackedRewardBody("rw-1", testSlot), false))

	api.setStatus("rw-1", http.StatusForbidden)
	require.NoError(t, deliver(w, trackedRewardBody("rw-1", testSlot), false))
	api.setStatus("rw-1", http.StatusBadGateway)
	require.Error(t, deliver(w, trackedRewardBody("rw-1", testSlot), true))

	assert.Equal(t, map[string]string{testSlot: outgress.RewardResultOK}, rewardResults(t, vc))
}

func TestExhaustedBatchFailsOnlyTheRewardsThatDidNotFinish(t *testing.T) {
	api, vc := newRewardsAPI(hydrate(), stretch()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc, withBatchStore(NewValkeyBatchStore(vc)))
	body := `{"type":"batch","broadcaster_id":"` + testBroadcaster + `","payload":{"id":"results-1","items":[` +
		trackedRewardBody("rw-1", "worked") + `,` + trackedRewardBody("rw-missing", "refused") + `,` + trackedRewardBody("rw-2", "stuck") + `,` + trackedRewardBody("rw-2", "never-reached") + `]}}`
	api.setStatus("rw-missing", http.StatusForbidden)
	api.setStatus("rw-2", http.StatusBadGateway)

	require.Error(t, deliver(w, body, false))
	require.Error(t, deliver(w, body, true))

	assert.Equal(t, map[string]string{
		"worked":        outgress.RewardResultOK,
		"refused":       outgress.RewardResultNotManageable,
		"stuck":         outgress.RewardResultUnconfirmed,
		"never-reached": outgress.RewardResultUnconfirmed,
	}, rewardResults(t, vc))
}

func TestRewardWithoutARunRecordsNothing(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)

	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50}`))

	keys, err := vc.Do(context.Background(), vc.B().Keys().Pattern("outgress:rewards:result:*").Build()).AsStrSlice()
	require.NoError(t, err)
	assert.Empty(t, keys)
}
