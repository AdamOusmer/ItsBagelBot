// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

const twoRewardBatch = `{"id":"rewards-1","items":[` +
	`{"type":"reward_update","reward_id":"rw-1","payload":{"cost":50,"reset_on_stream_end":true}},` +
	`{"type":"reward_update","reward_id":"rw-2","payload":{"cost":60}}]}`

func stretch() apiReward {
	return apiReward{ID: "rw-2", Title: "Stretch", Cost: 200, IsEnabled: true}
}

func (a *rewardsAPI) hook(onPatch, onGet func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onPatch, a.onGet = onPatch, onGet
}

func (a *rewardsAPI) streamerEdits(id string, cost int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rewards[id].Cost = cost
}

func TestBatchedRewardUpdateRetriesAfterATransientFailure(t *testing.T) {
	api, vc := newRewardsAPI(hydrate(), stretch()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc, withBatchStore(NewValkeyBatchStore(vc)))
	batch := testMessage{Type: "batch", Payload: twoRewardBatch}

	api.setStatus("rw-1", http.StatusBadGateway)
	require.Error(t, batch.send(w))
	api.setStatus("rw-1", 0)
	require.NoError(t, batch.send(w))

	assert.Equal(t, 50, api.state("rw-1").Cost, "the failed change reaches Twitch on redelivery")
	assert.Equal(t, 60, api.state("rw-2").Cost)

	require.NoError(t, sendRewardRestore(w))
	assert.Equal(t, 100, api.state("rw-1").Cost)
}

func TestFailedPatchSnapshotNeverOverwritesALaterEdit(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)

	api.setStatus("rw-1", http.StatusBadGateway)
	require.Error(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))
	assert.Empty(t, snapshotFields(t, vc), "a change that never applied owes no restore")

	api.setStatus("rw-1", 0)
	api.streamerEdits("rw-1", 777)
	api.resetCalls()
	require.NoError(t, sendRewardRestore(w))

	patches, _ := api.calls()
	assert.Empty(t, patches)
	assert.Equal(t, 777, api.state("rw-1").Cost)
}

func TestPatchThatAppliedButAnsweredFiveHundredKeepsTheFirstValue(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	api.failApplied = http.StatusBadGateway

	require.Error(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))
	assert.Equal(t, 50, api.state("rw-1").Cost)
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	_, gets := api.calls()
	assert.Equal(t, 1, gets, "the redelivery must not read the value it already changed")
	require.NoError(t, sendRewardRestore(w))
	assert.Equal(t, 100, api.state("rw-1").Cost)
}

func TestRefusedPatchForgetsTheStagedValue(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)

	api.setStatus("rw-1", http.StatusBadGateway)
	require.Error(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))
	api.setStatus("rw-1", http.StatusForbidden)
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	api.setStatus("rw-1", 0)
	api.streamerEdits("rw-1", 777)
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))
	require.NoError(t, sendRewardRestore(w))

	assert.Equal(t, 777, api.state("rw-1").Cost, "the restore puts back the value read for the change that worked")
}

func TestRestoreWaitsForARewardUpdateInFlight(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	first, second := rewardsWorker(t, api, vc), rewardsWorker(t, api, vc)
	require.NoError(t, sendRewardUpdate(first, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	inPatch, release := make(chan struct{}), make(chan struct{})
	var entered atomic.Bool
	api.hook(func() {
		if entered.CompareAndSwap(false, true) {
			close(inPatch)
			<-release
		}
	}, nil)

	updated := make(chan error, 1)
	go func() { updated <- sendRewardUpdate(first, "rw-1", `{"cost":25,"reset_on_stream_end":true}`) }()
	<-inPatch
	restored := make(chan error, 1)
	go func() { restored <- sendRewardRestore(second) }()
	time.Sleep(150 * time.Millisecond)
	close(release)

	require.NoError(t, <-updated)
	require.NoError(t, <-restored)
	assert.Equal(t, hydrate(), api.state("rw-1"), "a change landing after the restore could never be put back")
	assert.Empty(t, snapshotFields(t, vc))
}

func TestRestoreBusyBehindARewardUpdateIsRedelivered(t *testing.T) {
	api, vc := newRewardsAPI(hydrate()), valkeytest.Spawn(t)
	w := rewardsWorker(t, api, vc)
	require.NoError(t, sendRewardUpdate(w, "rw-1", `{"cost":50,"reset_on_stream_end":true}`))

	held := NewRewardSnapshots(vc).changeLock(testBroadcaster, "another-replica")
	won, err := held.Acquire(context.Background(), time.Minute)
	require.NoError(t, err)
	require.True(t, won)

	require.ErrorIs(t, sendRewardRestore(w), errRewardsBusy, "losing this delivery would leave the reward changed until the next stream")
	assert.Equal(t, 50, api.state("rw-1").Cost)

	require.NoError(t, held.Release(context.Background()))
	require.NoError(t, sendRewardRestore(w))
	assert.Equal(t, 100, api.state("rw-1").Cost)
}

func retryUntilAccepted(t *testing.T, send func() error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := send()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("never accepted: %v", err)
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func stressRewards(t *testing.T, api *rewardsAPI, vc valkey.Client) {
	t.Helper()
	replicas := []*Worker{rewardsWorker(t, api, vc), rewardsWorker(t, api, vc)}
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Go(func() {
			w := replicas[i%len(replicas)]
			retryUntilAccepted(t, func() error {
				return sendRewardUpdate(w, "rw-1", `{"cost":`+strconv.Itoa(10+i)+`,"reset_on_stream_end":true}`)
			})
		})
	}
	for i := range 3 {
		wg.Go(func() {
			time.Sleep(time.Duration(rand.IntN(8)) * time.Millisecond)
			retryUntilAccepted(t, func() error { return sendRewardRestore(replicas[i%len(replicas)]) })
		})
	}
	wg.Wait()
	retryUntilAccepted(t, func() error { return sendRewardRestore(replicas[0]) })
}

func TestConcurrentChangesAndRestoresAlwaysReturnToTheOriginal(t *testing.T) {
	jitter := func() { time.Sleep(time.Duration(rand.IntN(4)) * time.Millisecond) }
	vc := valkeytest.Spawn(t)
	for round := range 25 {
		api := newRewardsAPI(hydrate())
		api.hook(jitter, jitter)
		stressRewards(t, api, vc)
		require.Equal(t, 100, api.state("rw-1").Cost, "round %d left a change no snapshot can put back", round)
		require.Empty(t, snapshotFields(t, vc), "round %d", round)
	}
}
