// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"testing"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

func TestPullAckPolicyKnob(t *testing.T) {
	cases := []struct {
		knob    string
		policy  jsapi.AckPolicy
		pending int
	}{
		{"", jsapi.AckNonePolicy, 0},
		{"none", jsapi.AckNonePolicy, 0},
		{"all", jsapi.AckAllPolicy, defaultPullMaxAckPending},
	}
	for _, c := range cases {
		t.Setenv("NATS_PULL_ACK_POLICY", c.knob)
		cfg := pullConsumerConfig("twitch.ingress.event.premium", "sesame_twitch_ingress_event_premium")
		if cfg.AckPolicy != c.policy || cfg.MaxAckPending != c.pending {
			t.Fatalf("knob %q = %v/%d, want %v/%d", c.knob, cfg.AckPolicy, cfg.MaxAckPending, c.policy, c.pending)
		}
		if cfg.Replicas != defaultPullReplicas {
			t.Fatalf("knob %q replicas = %d, want %d: the ack policy must not change replication", c.knob, cfg.Replicas, defaultPullReplicas)
		}
	}
}

func TestPullAckNoneRecordsNoReceipt(t *testing.T) {
	t.Setenv("NATS_PULL_ACK_POLICY", "none")
	s := &pullSubscriber{desired: pullConsumerConfig("twitch.ingress.event.premium", "x")}
	s.noteReceipt(&fakePullMsg{})
	if s.takePending() != nil {
		t.Fatal("AckNone recorded a receipt for the floor ack")
	}
}

func TestPullBindWritesOnlyADriftedDurable(t *testing.T) {
	t.Setenv("NATS_PULL_CREATE_STAGGER", "0")
	desired := pullConsumerConfig("twitch.ingress.event.premium", "sesame_twitch_ingress_event_premium")
	drifted := desired
	drifted.AckWait = desired.AckWait / 2
	cases := []struct {
		name   string
		live   jsapi.ConsumerConfig
		writes int
	}{
		{"converged", desired, 0},
		{"drifted ack wait", drifted, 1},
	}
	for _, c := range cases {
		live := c.live
		live.Metadata = map[string]string{managedConsumerMetadata: "true", "_nats.req.level": "1"}
		js := &pullConsumerSpy{live: &jsapi.ConsumerInfo{Config: live}}
		if _, err := bindPullConsumer(context.Background(), js, "TWITCH_INGRESS", desired); err != nil {
			t.Fatal(err)
		}
		if len(js.created) != c.writes {
			t.Fatalf("%s: %d assignment writes, want %d", c.name, len(js.created), c.writes)
		}
	}
}

func TestPullCreateStaggerIsBounded(t *testing.T) {
	t.Setenv("NATS_PULL_CREATE_STAGGER", "0")
	started := time.Now()
	pullCreateStagger()
	if time.Since(started) > 50*time.Millisecond {
		t.Fatal("a zero stagger slept")
	}
	t.Setenv("NATS_PULL_CREATE_STAGGER", "20ms")
	started = time.Now()
	pullCreateStagger()
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("stagger of 20ms slept %s", elapsed)
	}
}

func TestAwaitPullLeaderSettlesOnAStableLeader(t *testing.T) {
	cfg := pullConsumerConfig("twitch.ingress.event.premium", "x")
	clustered := &pullConsumerHandle{info: &jsapi.ConsumerInfo{Config: cfg, Cluster: &jsapi.ClusterInfo{Leader: "nats-1"}}}
	started := time.Now()
	awaitPullLeader(context.Background(), clustered)
	if elapsed := time.Since(started); elapsed < pullLeaderPoll || elapsed > 10*pullLeaderPoll {
		t.Fatalf("stable leader settled in %s, want about one poll", elapsed)
	}
	single := &pullConsumerHandle{info: &jsapi.ConsumerInfo{Config: cfg}}
	started = time.Now()
	awaitPullLeader(context.Background(), single)
	if time.Since(started) > pullLeaderPoll {
		t.Fatal("a durable without cluster info should not wait")
	}
}

func TestPullConnectionsDefaultsToOne(t *testing.T) {
	t.Setenv("NATS_PULL_CONNECTIONS", "")
	if got := pullConnections(); got != 1 {
		t.Fatalf("default pull connections = %d, want 1", got)
	}
	t.Setenv("NATS_PULL_CONNECTIONS", "64")
	if got := pullConnections(); got != 32 {
		t.Fatalf("pull connections clamp = %d, want 32", got)
	}
	s := &pullSubscriber{consumer: &pullConsumerHandle{}}
	for i := range 3 {
		if s.handleFor(i) != s.consumer {
			t.Fatalf("loop %d without extra connections must use the lane consumer", i)
		}
	}
}
