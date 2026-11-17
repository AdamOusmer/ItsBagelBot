// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPullHealthReportsHorizonPressureAndRecovery(t *testing.T) {
	f := completionFixture(t)
	t.Setenv("NATS_PULL_ACK_WAIT", "100ms")
	spec := TwitchIngressPremiumStream
	spec.MaxAge, spec.Replicas = 100*time.Millisecond, 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
	sub := openSubscriber(t, f, "worker")
	messages := subscribe(t, sub, hotLane)
	f.appendMessage(t, hotLane, "held")
	msg := receive(t, messages)
	require.Eventually(t, func() bool { return !SubscriberHealthy(sub) }, 300*time.Millisecond, 10*time.Millisecond)
	msg.Ack()
	require.Eventually(t, func() bool { return SubscriberHealthy(sub) }, 31*time.Second, 50*time.Millisecond)
}

func TestPullHealthExcludesReleasedRetries(t *testing.T) {
	f := completionFixture(t)
	t.Setenv("NATS_PULL_NAK_DELAY", "5s")
	spec := TwitchIngressPremiumStream
	spec.MaxAge, spec.Replicas = 2*time.Second, 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
	sub := openSubscriber(t, f, "worker")
	require.NoError(t, Consume(t.Context(), nil, sub, hotLane, func(*Message) error {
		return errors.New("dependency unavailable")
	}, zap.NewNop()))
	f.appendMessage(t, hotLane, "retry")
	require.Never(t, func() bool { return !SubscriberHealthy(sub) }, 1800*time.Millisecond, 10*time.Millisecond)
}

func TestPullHealthClearsRetriesCompletedByAnotherPod(t *testing.T) {
	f := completionFixture(t)
	t.Setenv("NATS_PULL_NAK_DELAY", "50ms")
	spec := TwitchIngressPremiumStream
	spec.MaxAge, spec.Replicas = 4*time.Second, 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
	first := openSubscriber(t, f, "worker")
	messages := subscribe(t, first, hotLane)
	f.appendMessage(t, hotLane, "retry elsewhere")
	receive(t, messages).Nack()
	require.NoError(t, Consume(t.Context(), nil, first, hotLane, func(*Message) error {
		return errors.New("dependency unavailable")
	}, zap.NewNop()))
	second := openSubscriber(t, f, "worker")
	require.NoError(t, Consume(t.Context(), nil, second, hotLane, func(*Message) error { return nil }, zap.NewNop()))
	require.Eventually(t, func() bool { return f.messageCount(spec) == 0 }, 2*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return !SubscriberHealthy(first) }, 3500*time.Millisecond, 10*time.Millisecond)
}

func TestPullSlowQuarantineLeavesHealthyDeliveryRunnable(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler poison", true: "wire poison"}[malformed], func(t *testing.T) {
			f := completionFixture(t)
			nc, err := nats.Connect(f.url)
			require.NoError(t, err)
			defer nc.Close()
			_, err = nc.Subscribe(IngressDeadLetterPrefix+hotLane, func(*nats.Msg) {})
			require.NoError(t, err)
			require.NoError(t, nc.Flush())
			sub := openSubscriber(t, f, "worker")
			healthy := make(chan struct{}, 1)
			require.NoError(t, Consume(t.Context(), nil, sub, hotLane, func(msg *Message) error {
				if string(msg.Payload) != "healthy" {
					return Poison(errors.New("poison"))
				}
				healthy <- struct{}{}
				return nil
			}, zap.NewNop()))
			headers := poisonHeaders(malformed)
			f.appendMessage(t, hotLane, "poison", headers...)
			f.appendMessage(t, hotLane, "healthy")
			select {
			case <-healthy:
			case <-time.After(time.Second):
				t.Fatal("quarantine blocked healthy work")
			}
		})
	}
}

func poisonHeaders(malformed bool) []string {
	if malformed {
		return []string{"Traceparent", "one", "Traceparent", "two"}
	}
	return nil
}
