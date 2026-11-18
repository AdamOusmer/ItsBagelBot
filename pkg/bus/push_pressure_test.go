// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushLaneShedsMemberThatNeverHandlesDeliveries(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "20s")
		_, _ = f.open(t)
		started := time.Now()
		f.publishIDs(t, 0, 10)
		f.awaitPendingAcks(t, 10)
		_, fastInbox := f.open(t)
		var seen sync.Map
		go collectFailoverIDs(fastInbox, &seen)
		f.publishIDs(t, 10, 120)
		require.Eventually(t, func() bool { return publishedIDsSeen(&seen, 120) }, time.Until(started.Add(15*time.Second)), 25*time.Millisecond)
		assert.Less(t, time.Since(started), 15*time.Second)
		f.awaitEmpty(t)
	})
}

func (f pushFixture) awaitPendingAcks(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, f.pushName())
		return err == nil && info.NumAckPending == count
	}, 2*time.Second, 25*time.Millisecond)
}

func TestPushLaneResubscribesWhenUnbound(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		member, inbox := f.open(t)
		lane := f.pushMember(member)
		require.NoError(t, currentPushSubscription(lane).Unsubscribe())
		f.publishIDs(t, 0, 10)
		ackIDs(t, inbox, 0, 10)
		f.awaitEmpty(t)
		assert.True(t, f.consumer(t, f.pushName()).PushBound)
	})
}

func TestPushLaneReplacesSubscriptionWhenBrokerDeliveryStalls(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "3s")
		member, inbox := f.open(t)
		lane := f.pushMember(member)
		blocked := stallPushSubscription(t, lane)
		f.publishFlowBurst(t)
		require.Eventually(t, func() bool { return currentPushSubscription(lane) != blocked }, 3*time.Second, 25*time.Millisecond)
		receive(t, inbox).Ack()
	})
}

func stallPushSubscription(t *testing.T, lane *pushLane) *nats.Subscription {
	t.Helper()
	lane.mu.Lock()
	defer lane.mu.Unlock()
	require.NoError(t, lane.sub.Unsubscribe())
	blocked, err := lane.nc.QueueSubscribe(lane.desired.DeliverSubject, lane.desired.DeliverGroup, func(*nats.Msg) {})
	require.NoError(t, err)
	done := make(chan struct{})
	blocked.SetClosedHandler(func(string) { close(done) })
	lane.sub, lane.done = blocked, done
	require.NoError(t, lane.nc.Flush())
	return blocked
}

func currentPushSubscription(lane *pushLane) *nats.Subscription {
	lane.mu.Lock()
	defer lane.mu.Unlock()
	return lane.sub
}

func (f pushFixture) pushMember(member Subscriber) *pushLane {
	fleet := member.(*fleetSubscriber)
	fleet.mu.Lock()
	defer fleet.mu.Unlock()
	return fleet.flowLanes[f.spec.Name+"|"+f.subject()].sub.(*pullSubscriber).push
}

func TestPushLaneSteadyTrafficDoesNotCreateHandoverClaim(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "3s")
		group := uniqueGroup("push-traffic")
		f.seedPushConsumer(t, group)
		member := openSubscriber(t, f.busFixture, group)
		inbox := subscribe(t, member, f.subject())
		var seen sync.Map
		go collectFailoverIDs(inbox, &seen)
		subject := server.JSAdvisoryConsumerCreatedPre + "." + f.spec.Name + "." + durableName(group, f.subject()) + "_handover"
		claims, err := f.nc.SubscribeSync(subject)
		require.NoError(t, err)
		defer func() { _ = claims.Unsubscribe() }()
		require.NoError(t, f.nc.Flush())
		count := f.publishFor(t, 4*time.Second)
		require.Greater(t, count, 100)
		claim, err := claims.NextMsg(750 * time.Millisecond)
		if err == nil {
			t.Fatalf("unexpected handover claim: %s", claim.Data)
		}
		require.ErrorIs(t, err, nats.ErrTimeout)
		f.awaitAcked(t, pushConsumerName(group, f.subject()))
	})
}

func (f pushFixture) seedPushConsumer(t *testing.T, group string) {
	t.Helper()
	cfg := pushConsumerConfig(flowLaneConfig{group: group, subject: f.subject()})
	_, err := f.modern.CreatePushConsumer(t.Context(), f.spec.Name, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { f.deleteTestDurable(t, cfg.Name) })
}

func (f pushFixture) publishFor(t *testing.T, duration time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(duration)
	count := 0
	for time.Now().Before(deadline) {
		_, err := f.js.PublishAsync(f.subject(), []byte("steady traffic"))
		require.NoError(t, err)
		count++
	}
	select {
	case <-f.js.PublishAsyncComplete():
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for publish acknowledgements")
	}
	return count
}

func TestPushLaneStalledHeartbeatRecoversLostFlowControlReply(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "20s")
		seed, _ := f.open(t)
		require.NoError(t, seed.Close())
		raw, lostReply := f.dropFlowControl(t)
		response, err := f.nc.SubscribeSync(lostReply)
		require.NoError(t, err)
		defer func() { _ = response.Unsubscribe() }()
		require.NoError(t, f.nc.Flush())

		_, inbox := f.open(t)
		raw.Close()
		msg, err := response.NextMsg(4 * time.Second)
		require.NoError(t, err, "another queue member must answer the stalled heartbeat")
		assert.Empty(t, msg.Data)
		assert.Empty(t, msg.Header)
		first := receive(t, inbox)
		first.Ack()
	})
}

func (f pushFixture) dropFlowControl(t *testing.T) (*nats.Conn, string) {
	t.Helper()
	raw, err := nats.Connect(f.url)
	require.NoError(t, err)
	t.Cleanup(raw.Close)
	replies := make(chan string, 1)
	_, err = raw.QueueSubscribe(f.consumer(t, f.pushName()).Config.DeliverSubject, pushTestGroup, captureFlowControl(replies))
	require.NoError(t, err)
	require.NoError(t, raw.Flush())
	f.publishFlowBurst(t)
	select {
	case reply := <-replies:
		return raw, reply
	case <-time.After(5 * time.Second):
		t.Fatal("queue member did not receive a flow-control request")
		return nil, ""
	}
}

func (f pushFixture) publishFlowBurst(t *testing.T) {
	t.Helper()
	payload := make([]byte, 128*1024)
	for range 40 {
		_, err := f.js.Publish(f.subject(), payload)
		require.NoError(t, err)
	}
}

func captureFlowControl(replies chan<- string) nats.MsgHandler {
	return func(msg *nats.Msg) {
		if msg.Header.Get("Status") != "100" || msg.Reply == "" {
			return
		}
		select {
		case replies <- msg.Reply:
		default:
		}
	}
}
