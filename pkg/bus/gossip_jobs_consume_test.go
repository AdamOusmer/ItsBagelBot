// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func openJobs(t *testing.T) (*bus.GossipJobs, *nats.Conn) {
	t.Helper()
	nc := bustest.JetStream(t)
	jobs, err := bus.OpenGossipJobs(t.Context(), nc, 1)
	require.NoError(t, err)
	return jobs, nc
}

func publishJob(t *testing.T, jobs *bus.GossipJobs, id string) {
	t.Helper()
	require.NoError(t, jobs.Publish(t.Context(), bus.GossipJob{Kind: bus.GossipJobTTS, UserID: "42", EventID: id, Value: id}))
}

func queuedJobs(t *testing.T, nc *nats.Conn) uint64 {
	t.Helper()
	js, err := jsapi.New(nc)
	require.NoError(t, err)
	stream, err := js.Stream(t.Context(), bus.GossipJobsStream)
	require.NoError(t, err)
	info, err := stream.Info(t.Context())
	require.NoError(t, err)
	return info.State.Msgs
}

func TestGossipJobsConsumeRunsEveryJobConcurrently(t *testing.T) {
	const jobCount = 6
	ctx := t.Context()
	jobs, _ := openJobs(t)
	var started atomic.Int32
	release := make(chan struct{})
	stop, err := jobs.Consume(ctx, func(ctx context.Context, msg bus.GossipJobMessage) error {
		assert.Equal(t, bus.GossipJobTTS, msg.Kind)
		assert.Equal(t, "42", msg.UserID)
		started.Add(1)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	for i := range jobCount {
		publishJob(t, jobs, strconv.Itoa(i))
	}
	require.Eventually(t, func() bool { return started.Load() == jobCount }, 3*time.Second, 10*time.Millisecond)
	close(release)
}

func TestGossipJobsSettlement(t *testing.T) {
	table := []struct {
		name      string
		result    error
		wantCalls int32
	}{
		{"retry error redelivers", bus.JobRetryError{Delay: 50 * time.Millisecond, Err: errors.New("busy")}, 2},
		{"plain error redelivers", errors.New("boom"), 2},
		{"drop is terminated", bus.ErrDropJob, 1},
		{"success is acked", nil, 1},
	}
	for _, want := range table {
		t.Run(want.name, func(t *testing.T) {
			jobs, nc := openJobs(t)
			var calls atomic.Int32
			stop, err := jobs.Consume(t.Context(), func(context.Context, bus.GossipJobMessage) error {
				if calls.Add(1) > 1 {
					return nil
				}
				return want.result
			}, zap.NewNop())
			require.NoError(t, err)
			t.Cleanup(stop)

			publishJob(t, jobs, "job")
			require.Eventually(t, func() bool { return calls.Load() >= want.wantCalls }, 5*time.Second, 10*time.Millisecond)
			require.Eventually(t, func() bool { return queuedJobs(t, nc) == 0 }, 5*time.Second, 20*time.Millisecond)
			assert.Equal(t, want.wantCalls, calls.Load())
		})
	}
}

func TestGossipJobsRetryErrorHonorsDelay(t *testing.T) {
	const delay = 400 * time.Millisecond
	jobs, _ := openJobs(t)
	var first, second atomic.Int64
	stop, err := jobs.Consume(t.Context(), func(context.Context, bus.GossipJobMessage) error {
		if first.CompareAndSwap(0, time.Now().UnixNano()) {
			return bus.JobRetryError{Delay: delay, Err: errors.New("rate limited")}
		}
		second.CompareAndSwap(0, time.Now().UnixNano())
		return nil
	}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	publishJob(t, jobs, "job")
	require.Eventually(t, func() bool { return second.Load() != 0 }, 5*time.Second, 10*time.Millisecond)
	assert.GreaterOrEqual(t, time.Duration(second.Load()-first.Load()), delay-50*time.Millisecond)
}

func TestGossipJobsInvalidSubjectIsTerminated(t *testing.T) {
	jobs, nc := openJobs(t)
	var calls atomic.Int32
	stop, err := jobs.Consume(t.Context(), func(context.Context, bus.GossipJobMessage) error { calls.Add(1); return nil }, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	js, err := jsapi.New(nc)
	require.NoError(t, err)
	_, err = js.Publish(t.Context(), "gossip.jobs.tts", []byte("1"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return queuedJobs(t, nc) == 0 }, 5*time.Second, 20*time.Millisecond)
	assert.Zero(t, calls.Load())
}

func TestGossipJobsStopFinishesRunningAndHandsBackLateJobs(t *testing.T) {
	ctx := t.Context()
	jobs, nc := openJobs(t)
	var started, finished atomic.Int32
	release := make(chan struct{})
	serviceCtx, shutdown := context.WithCancel(ctx)
	stop, err := jobs.Consume(serviceCtx, func(ctx context.Context, _ bus.GossipJobMessage) error {
		started.Add(1)
		<-release
		if ctx.Err() == nil {
			finished.Add(1)
		}
		return nil
	}, zap.NewNop())
	require.NoError(t, err)

	publishJob(t, jobs, "running")
	require.Eventually(t, func() bool { return started.Load() == 1 }, 3*time.Second, 10*time.Millisecond)

	shutdown()
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("stop returned while a job was still running")
	case <-time.After(200 * time.Millisecond):
	}
	publishJob(t, jobs, "late")
	close(release)
	<-stopped
	assert.Equal(t, int32(1), started.Load())
	assert.Equal(t, int32(1), finished.Load())
	require.Eventually(t, func() bool { return queuedJobs(t, nc) == 1 }, 3*time.Second, 20*time.Millisecond)

	var taken atomic.Int32
	next, err := jobs.Consume(ctx, func(context.Context, bus.GossipJobMessage) error { taken.Add(1); return nil }, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(next)
	require.Eventually(t, func() bool { return taken.Load() == 1 }, 3*time.Second, 10*time.Millisecond)
}

const unsampledTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func TestGossipJobsSampleEveryJobOfAnUnsampledTrace(t *testing.T) {
	ctx := t.Context()
	app := serverlessTracingApp(t)
	jobs, _ := openJobs(t)

	type handled struct {
		traceID string
		sampled bool
	}
	seen := make(chan handled, 1)
	stop, err := jobs.Consume(ctx, func(ctx context.Context, _ bus.GossipJobMessage) error {
		txn := newrelic.FromContext(ctx)
		seen <- handled{traceID: txn.GetTraceMetadata().TraceID, sampled: txn.IsSampled()}
		return nil
	}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	origin := app.StartTransaction("alert")
	t.Cleanup(origin.End)
	origin.AcceptDistributedTraceHeaders(newrelic.TransportQueue, http.Header{
		"Traceparent": {"00-" + unsampledTraceID + "-00f067aa0ba902b7-00"},
		"Tracestate":  {"123@nr=0-0-123-456-00f067aa0ba902b7-e8b91a159289ff74-0-0.100000-1795288712686"},
	})
	require.False(t, origin.IsSampled())
	require.NoError(t, jobs.Publish(newrelic.NewContext(ctx, origin), bus.GossipJob{Kind: bus.GossipJobTTS, UserID: "42", EventID: "traced", Value: 1}))

	select {
	case got := <-seen:
		assert.Equal(t, handled{traceID: unsampledTraceID, sampled: true}, got)
	case <-time.After(3 * time.Second):
		t.Fatal("gossip job was not consumed")
	}
}

func serverlessTracingApp(t *testing.T) *newrelic.Application {
	t.Helper()
	app, err := newrelic.NewApplication(func(cfg *newrelic.Config) {
		cfg.ServerlessMode.Enabled = true
		cfg.ServerlessMode.AccountID = "123"
		cfg.ServerlessMode.TrustedAccountKey = "123"
		cfg.ServerlessMode.PrimaryAppID = "456"
		cfg.DistributedTracer.Enabled = true
	})
	require.NoError(t, err)
	monitor.Install(app)
	t.Cleanup(func() { monitor.Install(nil) })
	return app
}
