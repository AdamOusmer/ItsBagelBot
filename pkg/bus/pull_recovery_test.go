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

func TestPullHeartbeatCoversQueueAndCompletionWait(t *testing.T) {
	f := completionFixture(t)
	first := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	f.appendMessage(t, hotLane, "first")
	f.appendMessage(t, hotLane, "queued")
	msg := receive(t, first)
	completion := msg.DeferCompletion()
	name := PullLaneConsumerConfig("worker", hotLane).Name
	require.Eventually(t, func() bool {
		info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, name)
		return err == nil && info.NumAckPending == 2
	}, time.Second, time.Millisecond)
	second := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	requireSilence(t, second, 1300*time.Millisecond)
	queued := receive(t, first)
	assert.GreaterOrEqual(t, time.Since(queued.DeliveredAt()), time.Second)
	completion.Complete(nil)
	queued.Ack()
	require.Eventually(t, func() bool { return f.messageCount(TwitchIngressPremiumStream) == 0 }, time.Second, time.Millisecond)
}

func TestPullRetryDelayGrowsAcrossDeliveries(t *testing.T) {
	f := completionFixture(t)
	messages := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	f.appendMessage(t, hotLane, "retry")
	msg := receive(t, messages)
	for _, minimum := range []time.Duration{180 * time.Millisecond, 380 * time.Millisecond, 780 * time.Millisecond} {
		started := time.Now()
		msg.Nack()
		msg = receive(t, messages)
		assert.GreaterOrEqual(t, time.Since(started), minimum)
	}
	msg.Ack()
}

func TestPullVersionsCoexistWithoutDeletingInterest(t *testing.T) {
	f := completionFixture(t)
	oldName := durableName("worker", hotLane)
	_, err := f.js.AddConsumer(TwitchIngressPremiumStream.Name, &nats.ConsumerConfig{
		Durable: oldName, AckPolicy: nats.AckNonePolicy, FilterSubject: hotLane, MemoryStorage: true,
	})
	require.NoError(t, err)
	f.appendMessage(t, hotLane, "retained")
	newCfg := PullLaneConsumerConfig("worker", hotLane)
	sub := openSubscriber(t, f, "worker")
	_ = subscribe(t, sub, hotLane)
	before, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, newCfg.Name)
	require.NoError(t, err)
	for range 3 {
		_, err = f.js.UpdateConsumer(TwitchIngressPremiumStream.Name, &nats.ConsumerConfig{
			Durable: oldName, AckPolicy: nats.AckNonePolicy, FilterSubject: hotLane, MemoryStorage: true,
		})
		require.NoError(t, err)
		sub, openErr := NewSubscriber(f.url, "worker", zap.NewNop())
		require.NoError(t, openErr)
		messages := subscribe(t, sub, hotLane)
		require.NoError(t, sub.Close())
		assert.NotNil(t, messages)
	}
	after, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, newCfg.Name)
	require.NoError(t, err)
	assert.Equal(t, before.Created, after.Created)
	assert.Equal(t, uint64(1), f.messageCount(TwitchIngressPremiumStream))
}

func TestPullOlderContractCannotMutateNewerDurable(t *testing.T) {
	f := completionFixture(t)
	cfg := PullLaneConsumerConfig("worker", hotLane)
	info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, cfg.Name+"_staging")
	require.NoError(t, err)
	info.Config.Name, info.Config.Durable = cfg.Name, cfg.Name
	info.Config.FilterSubject = hotLane
	info.Config.Metadata[consumerContractMetadata] = "3"
	info, err = f.js.AddConsumer(TwitchIngressPremiumStream.Name, &info.Config)
	require.NoError(t, err)
	f.appendMessage(t, hotLane, "retained")
	sub := openSubscriber(t, f, "worker")
	_, err = sub.Subscribe(context.Background(), hotLane)
	require.ErrorContains(t, err, "newer contract")
	after, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, cfg.Name)
	require.NoError(t, err)
	assert.Equal(t, [2]any{info.Created, "3"}, [2]any{after.Created, after.Config.Metadata[consumerContractMetadata]})
	assert.Equal(t, uint64(1), f.messageCount(TwitchIngressPremiumStream))
}
