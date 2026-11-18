// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushLaneSlowMemberReleasesWindowAfterProgressExpiry(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "2s")
		t.Setenv("NATS_PUSH_MAX_ACK_PENDING", "15")
		slow, slowInbox := f.open(t)
		f.publishIDs(t, 0, 3)
		held := receive(t, slowInbox)
		t.Cleanup(func() { held.Nack() })
		_, fastInbox := f.open(t)
		var fast atomic.Uint64
		go func() {
			for msg := range fastInbox {
				msg.Ack()
				fast.Add(1)
			}
		}()
		f.publishIDs(t, 3, 120)
		f.awaitFullWindow(t, &fast)
		require.Eventually(t, func() bool { return !SubscriberHealthy(slow) }, 6*time.Second, 50*time.Millisecond)
		held.Nack()
		require.NoError(t, slow.Close())
		f.awaitEmpty(t)
		assert.Greater(t, fast.Load(), uint64(100))
	})
}

func (f pushFixture) awaitFullWindow(t *testing.T, fast *atomic.Uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, f.pushName())
		return err == nil && info.NumPending > 0 && info.NumAckPending == 15 && fast.Load() > 0
	}, 2*time.Second, 25*time.Millisecond)
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
	payload := make([]byte, 128*1024)
	for range 40 {
		_, err := f.js.Publish(f.subject(), payload)
		require.NoError(t, err)
	}
	select {
	case reply := <-replies:
		return raw, reply
	case <-time.After(5 * time.Second):
		t.Fatal("queue member did not receive a flow-control request")
		return nil, ""
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
