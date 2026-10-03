// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPullAckNoneRecordsNoReceipt(t *testing.T) {
	t.Setenv("NATS_PULL_ACK_POLICY", "none")
	s := &pullSubscriber{desired: pullConsumerConfig(hotLane, "x")}

	s.noteReceipt(&fakePullMsg{})

	assert.Nil(t, s.takePending(), "AckNone must not record a receipt for the floor ack")
}

func subscriberWithRebind(replacement jsapi.Consumer, failure error) (*pullSubscriber, *int) {
	attempts := 0
	s := &pullSubscriber{subject: hotLane, log: zap.NewNop()}
	s.rebind = func() (jsapi.Consumer, error) {
		attempts++
		if failure != nil {
			return nil, failure
		}
		return replacement, nil
	}
	return s, &attempts
}

func TestFetchErrorRebuildsALostDurable(t *testing.T) {
	replacement := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
	s, rebuilds := subscriberWithRebind(replacement, nil)

	running := s.noteFetchError(jsapi.ErrConsumerNotFound)

	assert.True(t, running, "a fetch error on a live binding must keep the pump loop running")
	assert.Equal(t, 1, *rebuilds, "one rebuild attempt after the durable went missing")
	assert.Same(t, replacement, s.consumer, "the pump loop is still bound to the consumer the server has deleted")
}

func TestOnlyALostDurableTriggersARebuild(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{jsapi.ErrConsumerLeadershipChanged, false},
		{jsapi.ErrNoHeartbeat, false},
		{errors.New("nats: timeout"), false},
		{jsapi.ErrConsumerNotFound, true},
		{jsapi.ErrConsumerDeleted, true},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			assert.Equal(t, tc.want, consumerGone(tc.err))
		})
	}
}

func TestAFailedRebuildKeepsTheLoopRunning(t *testing.T) {
	original := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
	s, rebuilds := subscriberWithRebind(nil, errors.New("nats: no responders"))
	s.consumer = original

	running := s.noteFetchError(jsapi.ErrConsumerDeleted)

	assert.True(t, running, "a failed rebuild must not stop the pump loop")
	assert.Equal(t, 1, *rebuilds)
	assert.Same(t, original, s.consumer, "a failed rebuild must not replace the binding with a consumer it never got")
}

func TestRebuildDoesNotDeadlockWithPrimaryHandleLookup(t *testing.T) {
	s := &pullSubscriber{log: zap.NewNop()}
	rebindEntered := make(chan struct{})
	releaseRebind := make(chan struct{})
	s.rebind = func() (jsapi.Consumer, error) {
		close(rebindEntered)
		<-releaseRebind
		return &pullConsumerHandle{}, nil
	}

	rebuilt := make(chan struct{})
	go func() {
		s.rebuildConsumer()
		close(rebuilt)
	}()
	<-rebindEntered

	fetched := make(chan struct{})
	go func() {
		s.handleFor(0)
		close(fetched)
	}()

	deadline := time.Now().Add(time.Second)
	for s.handleMu.TryLock() {
		s.handleMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("primary handle lookup never acquired handleMu")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseRebind)

	awaitSignal(t, rebuilt, "rebuild and primary handle lookup deadlocked")
	awaitSignal(t, fetched, "primary handle lookup did not complete after rebuild")
}

func TestLaneHealthSeparatesSilenceFromFailure(t *testing.T) {
	s, _ := subscriberWithRebind(nil, errors.New("nats: no responders"))
	erroringFor := func(age time.Duration) bool {
		s.errSince.Store(time.Now().Add(-age).UnixNano())
		return s.Healthy()
	}

	assert.True(t, s.Healthy(), "a lane that has never failed is healthy, however long it has been idle")
	assert.True(t, erroringFor(laneUnhealthyAfter/2), "a lane erroring for less than the grace window must still report ready")
	assert.False(t, erroringFor(2*laneUnhealthyAfter), "a lane stuck in its error path past the grace window must report unready")
	s.noteFetchProgress()
	assert.True(t, s.Healthy(), "a lane that read a message again must report ready")
}

func TestSubscriberHealthyAggregatesTheLanes(t *testing.T) {
	sick, _ := subscriberWithRebind(nil, nil)
	sick.errSince.Store(time.Now().Add(-2 * laneUnhealthyAfter).UnixNano())
	well, _ := subscriberWithRebind(nil, nil)
	fleet := &fleetSubscriber{flowLanes: map[string]*sharedFlowLane{"well": {sub: well}}}
	onlyHealthy := SubscriberHealthy(fleet)

	fleet.flowLanes["sick"] = &sharedFlowLane{sub: sick}

	assert.True(t, onlyHealthy, "a fleet whose only lane is healthy must report ready")
	assert.False(t, SubscriberHealthy(fleet), "one wedged lane must take the whole pod out of readiness")
	assert.True(t, SubscriberHealthy(&fleetSubscriber{}), "a subscriber with no lanes must report ready")
}

func TestPullConnectionsDefaultsToOne(t *testing.T) {
	t.Setenv("NATS_PULL_CONNECTIONS", "")
	assert.Equal(t, 1, pullConnections(), "default pull connections")
	t.Setenv("NATS_PULL_CONNECTIONS", "64")
	assert.Equal(t, 32, pullConnections(), "pull connections clamp")

	s := &pullSubscriber{consumer: &pullConsumerHandle{}}
	for i := range 3 {
		assert.Same(t, s.consumer, s.handleFor(i), "loop %d without extra connections must use the lane consumer", i)
	}
}

func TestPullCreateStaggerIsBounded(t *testing.T) {
	t.Setenv("NATS_PULL_CREATE_STAGGER", "0")
	started := time.Now()
	pullCreateStagger()
	assert.LessOrEqual(t, time.Since(started), 50*time.Millisecond, "a zero stagger must not sleep")

	t.Setenv("NATS_PULL_CREATE_STAGGER", "20ms")
	started = time.Now()
	pullCreateStagger()
	assert.LessOrEqual(t, time.Since(started), 200*time.Millisecond, "a stagger of 20ms must not sleep longer")
}

func TestAwaitPullLeaderSettlesOnAStableLeader(t *testing.T) {
	cfg := pullConsumerConfig("twitch.ingress.event.premium", "x")
	clustered := &pullConsumerHandle{info: &jsapi.ConsumerInfo{Config: cfg, Cluster: &jsapi.ClusterInfo{Leader: "nats-1"}}}
	single := &pullConsumerHandle{info: &jsapi.ConsumerInfo{Config: cfg}}

	started := time.Now()
	awaitPullLeader(context.Background(), clustered)
	settled := time.Since(started)
	started = time.Now()
	awaitPullLeader(context.Background(), single)
	unclustered := time.Since(started)

	assert.GreaterOrEqual(t, settled, pullLeaderPoll, "a stable leader settles after about one poll")
	assert.LessOrEqual(t, settled, 10*pullLeaderPoll)
	assert.LessOrEqual(t, unclustered, pullLeaderPoll, "a durable without cluster info must not wait")
}
