// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func deadLetterJetStream(t *testing.T) nats.JetStreamContext {
	t.Helper()
	s, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream()
	require.NoError(t, err)
	for _, spec := range []StreamSpec{BagelDataStream, BagelDeadLetterStream, {Name: "OTHER", Subjects: []string{"other.>"}}} {
		_, err := js.AddStream(&nats.StreamConfig{Name: spec.Name, Subjects: spec.Subjects, Duplicates: spec.Duplicates})
		require.NoError(t, err)
	}
	return js
}

func deadLetters(t *testing.T, js nats.JetStreamContext) uint64 {
	t.Helper()
	info, err := js.StreamInfo(BagelDeadLetterStream.Name)
	require.NoError(t, err)
	return info.State.Msgs
}

func TestDataLanesGrantOneDeferredDeliveryBeforeDeadLettering(t *testing.T) {
	delay := dataRetryDelay()
	require.EqualValues(t, 5, delay.deliveries())

	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"an early delivery nacks on the backoff step, not the requested delay", delay.nakDelay(1, 45*time.Second), 5 * time.Second},
		{"a last backoff delivery without a request terminates", delay.nakDelay(4, 0), terminateDelivery},
		{"a last backoff delivery honours a requested delay", delay.nakDelay(4, 45*time.Second), 45 * time.Second},
		{"a requested delay is capped at the deferral", delay.nakDelay(4, time.Hour), dataRetryDeferral},
		{"the deferred delivery terminates", delay.nakDelay(5, 45*time.Second), terminateDelivery},
		{"a plain capped delay ignores the requested delay at its limit", newMaxRetryDelay(time.Second, 4).nakDelay(4, 45*time.Second), terminateDelivery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

func TestDeferredLastDeliveryIsRedeliveredOnceThenDeadLettered(t *testing.T) {
	js := deadLetterJetStream(t)
	step := time.Millisecond
	delay := newBackoffRetryDelay([]time.Duration{step, step, step}).withDeferral(10 * step)
	core, logs := observer.New(zap.WarnLevel)
	s := &concurrentDurableSubscriber{js: js, stream: BagelDataStream.Name, consumer: "loyalty", delay: delay, log: zap.New(core)}
	_, err := js.Publish("data.loyalty.counters", []byte(`{"batch_id":"b1"}`))
	require.NoError(t, err)
	sub, err := js.PullSubscribe("data.loyalty.counters", "loyalty", nats.MaxDeliver(int(delay.deliveries())), nats.AckWait(time.Minute))
	require.NoError(t, err)

	for delivered := uint64(1); delivered <= delay.deliveries(); delivered++ {
		got, err := sub.Fetch(1, nats.MaxWait(2*time.Second))
		require.NoError(t, err, "delivery %d", delivered)
		meta, err := got[0].Metadata()
		require.NoError(t, err)
		require.Equal(t, delivered, meta.NumDelivered)
		require.Zero(t, deadLetters(t, js), "delivery %d", delivered)
		s.nack(got[0], time.Minute)
	}

	require.Equal(t, uint64(1), deadLetters(t, js))
	terminated := logs.FilterMessage("message terminated after max deliveries").All()
	require.Len(t, terminated, 1)
	require.Equal(t, map[string]any{
		"subject": "data.loyalty.counters", "stream": BagelDataStream.Name,
		"consumer": "loyalty", "deliveries": uint64(delay.deliveries()),
	}, terminated[0].ContextMap())
}
