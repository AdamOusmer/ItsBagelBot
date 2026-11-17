// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type laneSubscription struct {
	messages <-chan *Message
	err      error
}

type rolloutLane struct {
	group string
	spec  StreamSpec
}

func TestPullActivationPausesLegacyAndUsesFinalPosition(t *testing.T) {
	for _, tc := range []rolloutLane{
		{"worker", TwitchIngressPremiumStream},
		{"custom", TwitchIngressStandardStream},
	} {
		t.Run(tc.spec.Name, func(t *testing.T) {
			f := completionFixture(t)
			spec := tc.spec
			spec.Replicas = 1
			require.NoError(t, EnsureConsumerStreams(t.Context(), StreamSetup{URL: f.url, Group: tc.group, Specs: []StreamSpec{spec}, Log: zap.NewNop()}))
			tc.spec = spec
			runLegacyActivation(t, f, tc)
		})
	}
}

func runLegacyActivation(t *testing.T, f busFixture, lane rolloutLane) {
	t.Helper()
	spec, group := lane.spec, lane.group
	subject := spec.Subjects[0]
	name := durableName(group, subject)
	_, err := f.js.AddConsumer(spec.Name, &nats.ConsumerConfig{
		Durable: name, AckPolicy: nats.AckNonePolicy, FilterSubject: subject, MemoryStorage: true,
	})
	require.NoError(t, err)
	legacy, err := f.js.PullSubscribe(subject, name, nats.Bind(spec.Name, name))
	require.NoError(t, err)
	defer func() { _ = legacy.Unsubscribe() }()
	f.appendMessage(t, subject, "legacy effect")
	_, err = legacy.Fetch(1, nats.MaxWait(time.Second))
	require.NoError(t, err)
	fetched := make(chan error, 1)
	go func() { _, err := legacy.Fetch(1, nats.MaxWait(500*time.Millisecond)); fetched <- err }()
	require.Eventually(t, func() bool {
		info, err := f.js.ConsumerInfo(spec.Name, name)
		return err == nil && info.NumWaiting > 0
	}, time.Second, time.Millisecond)
	sub := openSubscriber(t, f, group)
	activated := make(chan laneSubscription, 1)
	go func() {
		messages, err := sub.Subscribe(context.Background(), subject)
		activated <- laneSubscription{messages, err}
	}()
	result := <-activated
	require.NoError(t, result.err)
	f.appendMessage(t, subject, "new effect")
	require.ErrorIs(t, <-fetched, nats.ErrTimeout)
	msg := receive(t, result.messages)
	assert.Equal(t, "new effect", string(msg.Payload))
	msg.Ack()
	legacyInfo, err := f.js.ConsumerInfo(spec.Name, name)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), legacyInfo.AckFloor.Stream)
	info, err := f.js.ConsumerInfo(spec.Name, PullLaneConsumerConfig(group, subject).Name)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), info.Config.OptStartSeq)
	_, err = f.js.ConsumerInfo(spec.Name, info.Name+"_staging")
	assert.ErrorIs(t, err, nats.ErrConsumerNotFound)
}

func TestPremiumActivationPausesSharedPredecessor(t *testing.T) {
	f := completionFixture(t)
	require.NoError(t, f.js.DeleteStream(TwitchIngressPremiumStream.Name))
	legacySpec := legacyTwitchIngressStream()
	legacySpec.Replicas = 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{legacySpec}, zap.NewNop()))
	name := durableName("worker", hotLane)
	_, err := f.js.AddConsumer(legacySpec.Name, &nats.ConsumerConfig{
		Durable: name, AckPolicy: nats.AckNonePolicy, FilterSubject: hotLane, MemoryStorage: true,
	})
	require.NoError(t, err)
	legacy, err := f.js.PullSubscribe(hotLane, name, nats.Bind(legacySpec.Name, name))
	require.NoError(t, err)
	defer func() { _ = legacy.Unsubscribe() }()
	for range 5 {
		f.appendMessage(t, hotLane, "legacy")
	}
	_, err = legacy.Fetch(5, nats.MaxWait(time.Second))
	require.NoError(t, err)
	reconcileWithOldPremiumBacklog(t, f, legacy)
	sub := openSubscriber(t, f, "worker")
	activated := make(chan laneSubscription, 1)
	go func() {
		messages, err := sub.Subscribe(t.Context(), hotLane)
		activated <- laneSubscription{messages, err}
	}()
	f.appendMessage(t, hotLane, "new stream sequence one")
	result := <-activated
	require.NoError(t, result.err)
	msg := receive(t, result.messages)
	assert.Equal(t, "new stream sequence one", string(msg.Payload))
	msg.Ack()
	info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, PullLaneConsumerConfig("worker", hotLane).Name)
	require.NoError(t, err)
	assert.Equal(t, [2]any{nats.DeliverAllPolicy, uint64(0)}, [2]any{info.Config.DeliverPolicy, info.Config.OptStartSeq})
}

func reconcileWithOldPremiumBacklog(t *testing.T, f busFixture, legacy *nats.Subscription) {
	t.Helper()
	for range 5 {
		f.appendMessage(t, hotLane, "unfetched old premium")
	}
	specs := []StreamSpec{TwitchIngressStream, TwitchIngressPremiumStream}
	for i := range specs {
		specs[i].Replicas = 1
	}
	done := make(chan error, 1)
	go func() { done <- EnsureStreams(t.Context(), f.url, specs, zap.NewNop()) }()
	require.Eventually(t, func() bool {
		info, err := f.js.StreamInfo(TwitchIngressStream.Name)
		return err == nil && len(info.Config.Subjects) == len(TwitchIngressStream.Subjects)
	}, time.Second, time.Millisecond)
	info, err := f.js.StreamInfo(TwitchIngressStream.Name)
	require.NoError(t, err)
	assert.Equal(t, legacyTwitchIngressStream().MaxBytes, info.Config.MaxBytes)
	batch, err := legacy.Fetch(5, nats.MaxWait(time.Second))
	require.NoError(t, err)
	assert.Len(t, batch, 5)
	require.NoError(t, <-done)
}

func TestPullUpdatesMutableContractFieldsInPlace(t *testing.T) {
	for _, field := range []string{"AckWait", "MaxAckPending", "Replicas"} {
		t.Run(field, func(t *testing.T) {
			f := completionFixture(t)
			name := PullLaneConsumerConfig("worker", hotLane).Name
			info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, name+"_staging")
			require.NoError(t, err)
			info.Config.Name, info.Config.Durable = name, name
			info.Config.FilterSubject = hotLane
			mutatePullField(&info.Config, field)
			before, err := f.js.AddConsumer(TwitchIngressPremiumStream.Name, &info.Config)
			require.NoError(t, err)
			messages := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
			f.appendMessage(t, hotLane, "healthy")
			receive(t, messages).Ack()
			after, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, name)
			require.NoError(t, err)
			assert.Equal(t, before.Created, after.Created)
			assert.Equal(t, [3]any{time.Second, -1, 1}, [3]any{after.Config.AckWait, after.Config.MaxAckPending, after.Config.Replicas})
		})
	}
}

func TestPullImmutableDriftRequiresNewDurable(t *testing.T) {
	for _, field := range []string{"AckPolicy", "ReplayPolicy", "MemoryStorage"} {
		t.Run(field, func(t *testing.T) {
			f := completionFixture(t)
			name := PullLaneConsumerConfig("worker", hotLane).Name
			info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, name+"_staging")
			require.NoError(t, err)
			info.Config.Name, info.Config.Durable = name, name
			info.Config.FilterSubject = hotLane
			mutatePullField(&info.Config, field)
			_, err = f.js.AddConsumer(TwitchIngressPremiumStream.Name, &info.Config)
			require.NoError(t, err)
			sub := openSubscriber(t, f, "worker")
			_, err = sub.Subscribe(t.Context(), hotLane)
			assert.ErrorContains(t, err, "newer versioned durable")
		})
	}
}

func mutatePullField(cfg *nats.ConsumerConfig, field string) {
	switch field {
	case "AckWait":
		cfg.AckWait = 3 * time.Second
	case "MaxAckPending":
		cfg.MaxAckPending = 7
	case "Replicas":
		cfg.Replicas = 0
	case "AckPolicy":
		cfg.AckPolicy = nats.AckNonePolicy
	case "ReplayPolicy":
		cfg.ReplayPolicy = nats.ReplayOriginalPolicy
	case "MemoryStorage":
		cfg.MemoryStorage = false
	}
}

func TestIngressDeadLettersCoexistWithLegacyWildcard(t *testing.T) {
	for _, ingressFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy first", true: "ingress first"}[ingressFirst], func(t *testing.T) {
			f := completionFixture(t)
			legacy := DataDeadLetterStream
			legacy.Subjects, legacy.Replicas = []string{"dlq.>"}, 1
			ingress := IngressDeadLetterStream
			ingress.Replicas = 1
			specs := []StreamSpec{legacy, ingress}
			if ingressFirst {
				specs[0], specs[1] = specs[1], specs[0]
			}
			for _, spec := range specs {
				require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
			}
			_, err := f.js.Publish(IngressDeadLetterPrefix+hotLane, []byte("poison"))
			require.NoError(t, err)
			assert.Equal(t, [2]uint64{0, 1}, [2]uint64{f.messageCount(legacy), f.messageCount(ingress)})
		})
	}
}
