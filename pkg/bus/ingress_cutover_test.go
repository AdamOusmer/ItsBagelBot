// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestIngressReconcilePreparesDurableBeforeSubjectMove(t *testing.T) {
	f := completionFixture(t)
	require.NoError(t, f.js.DeleteStream(TwitchIngressPremiumStream.Name))
	legacy := TwitchIngressStream
	legacy.Subjects = append([]string{hotLane}, legacy.Subjects...)
	legacy.Replicas = 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{legacy}, zap.NewNop()))
	nc, err := nats.Connect(f.url)
	require.NoError(t, err)
	defer nc.Close()
	trace, err := nc.SubscribeSync("$JS.>")
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	specs := []StreamSpec{TwitchIngressStream, TwitchIngressPremiumStream}
	for i := range specs {
		specs[i].Replicas = 1
	}
	require.NoError(t, EnsureStreams(t.Context(), f.url, specs, zap.NewNop()))
	requests := cutoverRequests(trace)
	created, durable, moved := -1, -1, -1
	for i, subject := range requests {
		switch {
		case strings.HasSuffix(subject, ".STREAM.CREATE.TWITCH_INGRESS_PREMIUM"):
			created = i
		case durable == -1 && strings.Contains(subject, ".CONSUMER.CREATE.TWITCH_INGRESS_PREMIUM."):
			durable = i
		case strings.HasSuffix(subject, ".STREAM.UPDATE.TWITCH_INGRESS"):
			moved = i
		}
	}
	assert.True(t, created >= 0 && durable > created, requests)
	assert.Greater(t, moved, durable, requests)
	f.appendMessage(t, hotLane, "before Sesame binds")
	assert.Equal(t, uint64(1), f.messageCount(TwitchIngressPremiumStream))
	info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, PullLaneConsumerConfig("worker", hotLane).Name+"_staging")
	require.NoError(t, err)
	assert.Equal(t, [2]any{nats.AckExplicitPolicy, nats.DeliverAllPolicy}, [2]any{info.Config.AckPolicy, info.Config.DeliverPolicy})
}

func TestPushLaneRetiresPullStagingWithOnlyPushDurable(t *testing.T) {
	t.Setenv("NATS_HOT_LANE_MODE", "pull")
	f := completionFixture(t)
	stage := PullLaneConsumerConfig("worker", hotLane).Name + "_staging"
	cfg, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, stage)
	require.NoError(t, err)

	t.Setenv("NATS_HOT_LANE_MODE", "push")
	_ = subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	_, err = f.js.AddConsumer(TwitchIngressPremiumStream.Name, &cfg.Config)
	require.NoError(t, err)
	spec := TwitchIngressPremiumStream
	spec.Replicas = 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))

	for _, name := range []string{stage, HotLaneConsumerName("worker", hotLane) + "_staging"} {
		_, err := f.js.ConsumerInfo(spec.Name, name)
		assert.ErrorIs(t, err, nats.ErrConsumerNotFound)
	}
}

func cutoverRequests(trace *nats.Subscription) []string {
	var subjects []string
	for {
		msg, err := trace.NextMsg(20 * time.Millisecond)
		if err != nil {
			return subjects
		}
		subjects = append(subjects, msg.Subject)
	}
}
