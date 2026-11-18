// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServerName string

func TestPushAlignmentStepdownKeepsEveryPublishedID(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		seed, _ := f.open(t)
		require.NoError(t, seed.Close())
		stream := f.readyStream(t)
		name := hotDurable(f.pushName())
		other := testServerName(stream.Cluster.Replicas[0].Name)
		f.moveConsumerLeader(t, name, other)
		f.waitConsumerLeader(t, name, other, 3*time.Second)
		t.Setenv("NATS_LANE_ALIGN", "on")
		_, inbox := f.open(t)
		var seen sync.Map
		go collectFailoverIDs(inbox, &seen)
		stop, published := f.publishDuringFailover(t)
		f.waitConsumerLeader(t, name, testServerName(stream.Cluster.Leader), 6*time.Second)
		close(stop)
		count := <-published
		require.Positive(t, count)
		f.publishIDs(t, count, count+20)
		require.Eventually(t, func() bool { return publishedIDsSeen(&seen, count+20) }, 12*time.Second, 25*time.Millisecond)
		f.awaitEmpty(t)
	})
}

func TestLaneAlignmentFollowsStreamLeaderWithCooldown(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "6s")
		if f.spec.Name == TwitchIngressStandardStream.Name {
			t.Setenv("NATS_HOT_LANE_MODE", "pull")
		}
		name := hotDurable(HotLaneConsumerName(pushTestGroup, f.subject()))
		first, _ := f.open(t)
		require.NoError(t, first.Close())
		stream := f.readyStream(t)
		other := testServerName(stream.Cluster.Replicas[0].Name)
		f.moveConsumerLeader(t, name, other)
		f.waitConsumerLeader(t, name, other, 3*time.Second)

		advisory, err := f.nc.SubscribeSync("$JS.EVENT.ADVISORY.CONSUMER.LEADER_ELECTED." + f.spec.Name + "." + string(name))
		require.NoError(t, err)
		defer func() { _ = advisory.Unsubscribe() }()
		require.NoError(t, f.nc.Flush())
		t.Setenv("NATS_LANE_ALIGN", "on")
		t.Setenv("NATS_LANE_ALIGN_INTERVAL", "4s")
		sub, err := NewSubscriber(f.url, pushTestGroup, zaptest.NewLogger(t))
		require.NoError(t, err)
		t.Cleanup(func() { _ = sub.Close() })
		_ = subscribe(t, sub, f.subject())
		f.waitConsumerLeader(t, name, testServerName(stream.Cluster.Leader), 3*time.Second)
		_, err = advisory.NextMsg(time.Second)
		require.NoError(t, err)

		f.forceStepDown(t, "$JS.API.STREAM.LEADER.STEPDOWN."+f.spec.Name, other)
		f.waitStreamLeader(t, other)
		_, err = advisory.NextMsg(time.Second)
		assert.ErrorIs(t, err, nats.ErrTimeout, "consumer moved again inside the alignment interval")
		f.waitConsumerLeader(t, name, other, 6*time.Second)
	})
}

func (f pushFixture) readyStream(t *testing.T) *nats.StreamInfo {
	t.Helper()
	var stream *nats.StreamInfo
	require.Eventually(t, func() bool {
		info, err := f.js.StreamInfo(f.spec.Name, nats.MaxWait(500*time.Millisecond))
		stream = info
		return err == nil && streamFullyReplicated(info)
	}, 12*time.Second, 50*time.Millisecond)
	return stream
}

func streamFullyReplicated(info *nats.StreamInfo) bool {
	if info.Cluster == nil {
		return false
	}
	if info.Cluster.Leader == "" || len(info.Cluster.Replicas) != 2 {
		return false
	}
	for _, peer := range info.Cluster.Replicas {
		if !peer.Current {
			return false
		}
	}
	return true
}

func (f pushFixture) waitStreamLeader(t *testing.T, name testServerName) {
	t.Helper()
	require.Eventually(t, func() bool {
		info, err := f.js.StreamInfo(f.spec.Name, nats.MaxWait(500*time.Millisecond))
		known := err == nil && info.Cluster != nil
		return known && info.Cluster.Leader == string(name)
	}, 5*time.Second, 25*time.Millisecond)
}

func (f pushFixture) waitConsumerLeader(t *testing.T, durable hotDurable, server testServerName, wait time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, string(durable))
		return err == nil && consumerLeaderName(info) == string(server)
	}, wait, 25*time.Millisecond)
}

func (f pushFixture) moveConsumerLeader(t *testing.T, durable hotDurable, preferred testServerName) {
	t.Helper()
	if consumerLeaderName(f.consumer(t, string(durable))) != string(preferred) {
		f.forceStepDown(t, "$JS.API.CONSUMER.LEADER.STEPDOWN."+f.spec.Name+"."+string(durable), preferred)
	}
}

func (f pushFixture) forceStepDown(t *testing.T, subject string, preferred testServerName) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"placement": map[string]testServerName{"preferred": preferred}})
	require.NoError(t, err)
	reply, err := f.nc.Request(subject, body, time.Second)
	require.NoError(t, err)
	require.Contains(t, string(reply.Data), `"success":true`)
}
