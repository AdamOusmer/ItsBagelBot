// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushLaneHandoverCarriesAckFloor(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		_, push := f.forwardHandover(t)
		ackIDs(t, push, 3, 9)
		f.awaitEmpty(t)
	})
}

func (f pushFixture) forwardHandover(t *testing.T) (Subscriber, <-chan *Message) {
	t.Helper()
	t.Setenv("NATS_HOT_LANE_MODE", "pull")
	pull, inbox := f.open(t)
	f.publishIDs(t, 0, 3)
	ackIDs(t, inbox, 0, 3)
	floor := f.awaitAcked(t, f.pullName())

	f.publishIDs(t, 3, 5)
	held := []*Message{receive(t, inbox), receive(t, inbox)}
	t.Cleanup(func() { ackHeld(held) })
	f.publishIDs(t, 5, 9)
	t.Setenv("NATS_HOT_LANE_MODE", "push")
	push, deliveries := f.open(t)
	old, current := f.consumer(t, f.pullName()), f.consumer(t, f.pushName())
	assert.Equal(t, [3]any{true, floor, floor + 1}, [3]any{old.Paused, old.AckFloor.Stream, current.Config.OptStartSeq})
	ackHeld(held)
	require.NoError(t, pull.Close())
	return push, deliveries
}

func ackHeld(held []*Message) {
	for _, msg := range held {
		msg.Ack()
	}
}

func (f pushFixture) awaitAcked(t *testing.T, name string) uint64 {
	t.Helper()
	stream, err := f.js.StreamInfo(f.spec.Name, nats.MaxWait(time.Second))
	require.NoError(t, err)
	var floor uint64
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, name)
		if err != nil {
			return false
		}
		floor = info.AckFloor.Stream
		empty := info.NumAckPending == 0 && info.NumPending == 0
		return empty && floor == stream.State.LastSeq
	}, 3*time.Second, 25*time.Millisecond)
	return floor
}

func TestPushLaneRollbackWaitsForLivePushMember(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "3s")
		push, deliveries := f.forwardHandover(t)
		ackIDs(t, deliveries, 3, 9)
		floor := f.awaitAcked(t, f.pushName())
		t.Setenv("NATS_HOT_LANE_MODE", "pull")
		_, pull := f.open(t)
		requireSilence(t, pull, 1600*time.Millisecond)
		assert.False(t, f.consumer(t, f.pushName()).Paused)

		require.NoError(t, push.Close())
		f.publishIDs(t, 9, 13)
		ackIDs(t, pull, 9, 13)
		old, new := f.consumer(t, f.pushName()), f.consumer(t, f.pullName())
		assert.Equal(t, [3]any{true, false, floor + 1}, [3]any{old.Paused, new.Paused, new.Config.OptStartSeq})
	})
}

func TestPushStandbyDoesNotTakeOverFromStalledPullMember(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_FETCH_LOOPS", "1")
		t.Setenv("NATS_PULL_FETCH_BATCH", "2")
		push, deliveries := f.forwardHandover(t)
		ackIDs(t, deliveries, 3, 9)
		f.awaitEmpty(t)
		require.NoError(t, push.Close())

		t.Setenv("NATS_HOT_LANE_MODE", "pull")
		_, pull := f.open(t)
		f.publishIDs(t, 9, 10)
		ackIDs(t, pull, 9, 10)
		before := f.consumer(t, f.pullName()).Created
		t.Setenv("NATS_HOT_LANE_MODE", "push")
		_, standby := f.open(t)
		f.verifyStalledPullRemainsActive(t, pull, standby)
		assert.Equal(t, before, f.consumer(t, f.pullName()).Created)
	})
}

func (f pushFixture) verifyStalledPullRemainsActive(t *testing.T, pull, standby <-chan *Message) {
	t.Helper()
	f.publishIDs(t, 10, 14)
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, f.pullName())
		stalled := err == nil && info.NumWaiting == 0
		return stalled && info.NumAckPending > 0
	}, 2*time.Second, 25*time.Millisecond)
	requireSilence(t, standby, 4*time.Second)
	assert.True(t, f.consumer(t, f.pushName()).Paused)
	assert.False(t, f.consumer(t, f.pullName()).Paused)
	ackIDs(t, pull, 10, 14)
	f.awaitAcked(t, f.pullName())
}

func TestLanePullWaitingCountsRequestsWhenPausedAndActive(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_HOT_LANE_MODE", "pull")
		sub, _ := f.open(t)
		require.NoError(t, sub.Close())
		handle, err := f.modern.Consumer(t.Context(), f.spec.Name, f.pullName())
		require.NoError(t, err)
		for _, paused := range []bool{false, true} {
			require.NoError(t, f.probePullWaiting(t, handle, paused))
		}
	})
}

func (f pushFixture) probePullWaiting(t *testing.T, handle jsapi.Consumer, paused bool) error {
	t.Helper()
	if paused {
		_, err := f.modern.PauseConsumer(t.Context(), f.spec.Name, f.pullName(), time.Now().Add(time.Minute))
		require.NoError(t, err)
	}
	batch, err := handle.Fetch(1, jsapi.FetchMaxWait(time.Second))
	if err != nil {
		return err
	}
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, f.pullName())
		expectedState := err == nil && info.Paused == paused
		return expectedState && info.NumWaiting > 0
	}, 700*time.Millisecond, 25*time.Millisecond)
	for msg := range batch.Messages() {
		t.Errorf("empty consumer delivered %s", msg.Subject())
	}
	return batch.Error()
}
