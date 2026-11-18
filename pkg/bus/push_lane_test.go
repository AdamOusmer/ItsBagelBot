// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pushTestGroup = "worker"

type pushFixture struct {
	busFixture
	spec StreamSpec
}

func hotLaneTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NATS_INTEGRATION_URL", "")
	for name, value := range map[string]string{
		"NATS_HOT_LANE_MODE":       "push",
		"NATS_LANE_ALIGN":          "off",
		"NATS_PULL_REPLICAS":       "3",
		"NATS_PULL_CREATE_STAGGER": "0",
		"NATS_PULL_FETCH_BATCH":    "10",
		"NATS_PULL_ACK_WAIT":       "6s",
		"NATS_INGRESS_PARTITION":   "on",
	} {
		t.Setenv(name, value)
	}
}

func forPushStreams(t *testing.T, run func(*testing.T, pushFixture)) {
	t.Helper()
	for _, spec := range []StreamSpec{TwitchIngressPremiumStream, TwitchIngressStandardStream} {
		t.Run(spec.Name, func(t *testing.T) {
			hotLaneTestEnv(t)
			f := pushFixture{busFixture: newBusFixture(t), spec: spec}
			for _, name := range []string{f.pullName(), f.pushName()} {
				f.deleteTestDurable(t, name)
				t.Cleanup(func() { f.deleteTestDurable(t, name) })
			}
			run(t, f)
		})
	}
}

func (f pushFixture) subject() string  { return f.spec.Subjects[0] }
func (f pushFixture) pullName() string { return durableName(pushTestGroup, f.subject()) + "_v2" }
func (f pushFixture) pushName() string { return durableName(pushTestGroup, f.subject()) + "_v3" }

func (f pushFixture) deleteTestDurable(t *testing.T, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := f.modern.DeleteConsumer(ctx, f.spec.Name, name)
	if !consumerGone(err) {
		require.NoError(t, err)
	}
}

func (f pushFixture) open(t *testing.T) (Subscriber, <-chan *Message) {
	t.Helper()
	sub := openSubscriber(t, f.busFixture, pushTestGroup)
	return sub, subscribe(t, sub, f.subject())
}

func (f pushFixture) consumer(t *testing.T, name string) *jsapi.ConsumerInfo {
	t.Helper()
	var result *jsapi.ConsumerInfo
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		result, err = f.lookup(t, name)
		assert.NoError(c, err)
	}, 12*time.Second, 25*time.Millisecond)
	return result
}

func (f pushFixture) lookup(t *testing.T, name string) (*jsapi.ConsumerInfo, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if strings.HasSuffix(name, "_v3") {
		return f.lookupPush(ctx, name)
	}
	handle, err := f.modern.Consumer(ctx, f.spec.Name, name)
	if err != nil {
		return nil, err
	}
	return handle.CachedInfo(), nil
}

func (f pushFixture) lookupPush(ctx context.Context, name string) (*jsapi.ConsumerInfo, error) {
	handle, err := f.modern.PushConsumer(ctx, f.spec.Name, name)
	if err != nil {
		return nil, err
	}
	return handle.CachedInfo(), nil
}

func (f pushFixture) publishIDs(t *testing.T, start, end int) {
	t.Helper()
	for id := start; id < end; id++ {
		value := strconv.Itoa(id)
		f.appendMessage(t, f.subject(), value, nats.MsgIdHdr, nats.NewInbox())
	}
}

func ackIDs(t *testing.T, inbox <-chan *Message, start, end int) {
	t.Helper()
	want := make(map[string]bool)
	for id := start; id < end; id++ {
		want[strconv.Itoa(id)] = true
	}
	for range end - start {
		msg := receive(t, inbox)
		value := string(msg.Payload)
		msg.Ack()
		require.True(t, want[value], "unexpected or duplicate payload %s", value)
		delete(want, value)
	}
}

func TestPushLaneShape(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_MAX_ACK_PENDING", "17")
		_, _ = f.open(t)
		info := f.consumer(t, HotLaneConsumerName(pushTestGroup, f.subject()))
		cfg := info.Config
		assert.Equal(t, [9]any{jsapi.AckExplicitPolicy, true, time.Second, pushTestGroup, 3, true, "3", -1, time.Duration(0)},
			[9]any{cfg.AckPolicy, cfg.FlowControl, cfg.IdleHeartbeat, cfg.DeliverGroup, cfg.Replicas, cfg.MemoryStorage,
				cfg.Metadata[consumerContractMetadata], cfg.MaxAckPending, cfg.InactiveThreshold})
		require.NotNil(t, info.Cluster)
		assert.Len(t, info.Cluster.Replicas, 2)
	})
}

func TestPushLaneConvergesMutableFieldsAndRejectsImmutableDrift(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		first, _ := f.open(t)
		created := f.consumer(t, f.pushName()).Created
		require.NoError(t, first.Close())
		t.Setenv("NATS_PULL_ACK_WAIT", "7s")
		second, _ := f.open(t)
		current := f.consumer(t, f.pushName())
		assert.Equal(t, [3]any{created, 7 * time.Second, -1}, [3]any{current.Created, current.Config.AckWait, current.Config.MaxAckPending})
		require.NoError(t, second.Close())
		f.rejectPushDrift(t, current.Config)
	})
}

func (f pushFixture) rejectPushDrift(t *testing.T, cfg jsapi.ConsumerConfig) {
	t.Helper()
	f.deleteTestDurable(t, cfg.Name)
	cfg.FlowControl = false
	_, err := f.modern.CreatePushConsumer(t.Context(), f.spec.Name, cfg)
	require.NoError(t, err)
	sub := openSubscriber(t, f.busFixture, pushTestGroup)
	_, err = sub.Subscribe(t.Context(), f.subject())
	assert.ErrorContains(t, err, "requires a newer versioned durable")
}

func TestPushLaneQueueSharesDeliveries(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		_, first := f.open(t)
		_, second := f.open(t)
		f.publishIDs(t, 0, 1000)
		seen, counts := collectSharedPush(t, first, second, 1000)
		assert.Len(t, seen, 1000)
		assert.Positive(t, min(counts[0], counts[1]))
		f.awaitEmpty(t)
	})
}

func collectSharedPush(t *testing.T, first, second <-chan *Message, total int) (map[string]bool, [2]int) {
	t.Helper()
	seen := make(map[string]bool)
	var counts [2]int
	for range total {
		msg, member := receiveSharedPush(t, first, second)
		id := string(msg.Payload)
		msg.Ack()
		require.False(t, seen[id], "duplicate payload %s", id)
		seen[id] = true
		counts[member]++
	}
	return seen, counts
}

func receiveSharedPush(t *testing.T, first, second <-chan *Message) (*Message, int) {
	t.Helper()
	select {
	case msg := <-first:
		return msg, 0
	case msg := <-second:
		return msg, 1
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for either queue member")
		return nil, 0
	}
}

func (f pushFixture) awaitEmpty(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		info, err := f.lookup(t, f.pushName())
		knownEmpty := err == nil && info.NumPending == 0
		return knownEmpty && info.NumAckPending == 0
	}, 6*time.Second, 25*time.Millisecond)
}

func TestPushLaneDrainsQueuedDeliveriesBeforeAckWait(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "12s")
		first, _ := f.open(t)
		f.publishIDs(t, 0, 100)
		require.Eventually(t, func() bool {
			info, err := f.lookup(t, f.pushName())
			return err == nil && info.NumAckPending == 100
		}, 5*time.Second, 25*time.Millisecond)
		_, second := f.open(t)
		started := time.Now()
		require.NoError(t, first.Close())
		ackIDs(t, second, 0, 100)
		assert.Less(t, time.Since(started), 8*time.Second)
	})
}

func TestPushLaneRebuildsDeletedConsumer(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		t.Setenv("NATS_PULL_ACK_WAIT", "3s")
		_, inbox := f.open(t)
		before := f.consumer(t, f.pushName()).Created
		f.deleteTestDurable(t, f.pushName())
		require.Eventually(t, func() bool {
			info, err := f.lookup(t, f.pushName())
			return err == nil && info.Created.After(before)
		}, time.Second, 25*time.Millisecond)
		f.publishIDs(t, 0, 1)
		ackIDs(t, inbox, 0, 1)
	})
}

func TestPushConsumerDeletionDoesNotSend409ToDeliverSubject(t *testing.T) {
	forPushStreams(t, func(t *testing.T, f pushFixture) {
		_, inbox := f.open(t)
		tap, err := f.nc.SubscribeSync(f.consumer(t, f.pushName()).Config.DeliverSubject)
		require.NoError(t, err)
		defer func() { _ = tap.Unsubscribe() }()
		require.NoError(t, f.nc.Flush())
		f.publishIDs(t, 0, 1)
		ackIDs(t, inbox, 0, 1)
		_, err = tap.NextMsg(time.Second)
		require.NoError(t, err)
		f.deleteTestDurable(t, f.pushName())
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			msg, err := tap.NextMsg(time.Until(deadline))
			if err != nil {
				require.ErrorIs(t, err, nats.ErrTimeout)
				return
			}
			require.NotEqual(t, "409 Consumer Deleted", fmt.Sprint(msg.Header.Get("Status"), " ", msg.Header.Get("Description")))
		}
	})
}
