// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type rolloutEffects struct {
	sync.Mutex
	seen map[string]int
}

func (r *rolloutEffects) record(body []byte) {
	r.Lock()
	r.seen[string(body)]++
	r.Unlock()
}

func (r *rolloutEffects) complete() bool {
	r.Lock()
	defer r.Unlock()
	return len(r.seen) == 100
}

func (r *rolloutEffects) verify(t *testing.T) {
	t.Helper()
	r.Lock()
	defer r.Unlock()
	for body, count := range r.seen {
		assert.Equal(t, 1, count, body)
	}
}

func consumeLegacyRollout(legacy *nats.Subscription, effects *rolloutEffects, done chan<- struct{}, stop <-chan struct{}) {
	defer close(done)
	for rolloutLive(stop) {
		batch, err := legacy.Fetch(10, nats.MaxWait(200*time.Millisecond))
		for _, msg := range batch {
			effects.record(msg.Data)
		}
		if err != nil && err != nats.ErrTimeout {
			return
		}
	}
}

func rolloutLive(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return false
	default:
		return true
	}
}

func joinRollout(ctx context.Context, setup StreamSetup, sub Subscriber, effects *rolloutEffects) error {
	if err := EnsureConsumerStreams(ctx, setup); err != nil {
		return err
	}
	return Consume(ctx, nil, sub, setup.Specs[0].Subjects[0], func(msg *Message) error { effects.record(msg.Payload); return nil }, zap.NewNop())
}

func TestPullThreePodsConvergeStagingWhileLegacyConsumes(t *testing.T) {
	f := completionFixture(t)
	spec := TwitchIngressStandardStream
	spec.Replicas = 1
	subject := spec.Subjects[0]
	_, err := f.js.AddStream(&nats.StreamConfig{Name: spec.Name, Subjects: spec.Subjects, Storage: nats.MemoryStorage})
	require.NoError(t, err)
	name := durableName("custom", subject)
	legacy, err := f.js.PullSubscribe(subject, name, nats.BindStream(spec.Name), nats.AckNone())
	require.NoError(t, err)
	t.Cleanup(func() { _ = legacy.Unsubscribe() })
	nc, err := nats.Connect(f.url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	trace, err := nc.SubscribeSync("$JS.API.>")
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	effects := &rolloutEffects{seen: map[string]int{}}
	drained := make(chan struct{})
	stop := make(chan struct{})
	go consumeLegacyRollout(legacy, effects, drained, stop)
	for i := range 30 {
		f.appendMessage(t, subject, fmt.Sprint(i))
	}
	var pods sync.WaitGroup
	errors := make(chan error, 3)
	setup := StreamSetup{URL: f.url, Specs: []StreamSpec{spec}, Group: "custom", Log: zap.NewNop()}
	for range 3 {
		sub, err := NewSubscriber(f.url, "custom", zap.NewNop())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, sub.Close()) })
		pods.Go(func() {
			errors <- joinRollout(t.Context(), setup, sub, effects)
		})
	}
	for i := 30; i < 100; i++ {
		f.appendMessage(t, subject, fmt.Sprint(i))
	}
	pods.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Eventually(t, effects.complete, 2*time.Second, time.Millisecond)
	close(stop)
	<-drained
	effects.verify(t)
	requests := cutoverRequests(trace)
	assert.Contains(t, requests, "$JS.API.CONSUMER.PAUSE."+spec.Name+"."+name)
	_, err = f.js.ConsumerInfo(spec.Name, PullLaneConsumerConfig("custom", subject).Name+"_staging")
	assert.ErrorIs(t, err, nats.ErrConsumerNotFound)
}
