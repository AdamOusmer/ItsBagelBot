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

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestTTSJobsConsumeWithBoundedParallelism(t *testing.T) {
	const workers, jobCount = 2, 4
	ctx := t.Context()
	jobs, err := bus.OpenTTSJobs(ctx, bustest.JetStream(t), 1)
	require.NoError(t, err)

	var started atomic.Int32
	release := make(chan struct{})
	stop, err := jobs.Consume(ctx, func(ctx context.Context, _ []byte) error {
		started.Add(1)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, workers, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	for i := range jobCount {
		job := bus.TTSMessage{BroadcasterID: "42", EventID: strconv.Itoa(i), Value: i}
		require.NoError(t, jobs.Publish(ctx, job))
	}
	require.Eventually(t, func() bool { return started.Load() == workers }, 3*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return started.Load() > workers }, 100*time.Millisecond, 10*time.Millisecond)

	close(release)
	require.Eventually(t, func() bool { return started.Load() == jobCount }, 3*time.Second, 10*time.Millisecond)
}

func TestTTSJobsShutdownReleasesPulledJobsAndFinishesInFlight(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	jobs, err := bus.OpenTTSJobs(ctx, nc, 1)
	require.NoError(t, err)

	var started, finished atomic.Int32
	release := make(chan struct{})
	serviceCtx, shutdown := context.WithCancel(ctx)
	stop, err := jobs.Consume(serviceCtx, func(ctx context.Context, _ []byte) error {
		started.Add(1)
		<-release
		if ctx.Err() == nil {
			finished.Add(1)
		}
		return nil
	}, 2, zap.NewNop())
	require.NoError(t, err)

	for i := range 4 {
		require.NoError(t, jobs.Publish(ctx, bus.TTSMessage{BroadcasterID: "42", EventID: strconv.Itoa(i), Value: i}))
	}
	require.Eventually(t, func() bool { return started.Load() == 2 }, 3*time.Second, 10*time.Millisecond)

	shutdown()
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("shutdown returned while a job was still in flight")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	<-stopped
	assert.Equal(t, int32(2), started.Load())
	assert.Equal(t, int32(2), finished.Load())

	var taken atomic.Int32
	next, err := jobs.Consume(ctx, func(context.Context, []byte) error { taken.Add(1); return nil }, 2, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(next)
	require.Eventually(t, func() bool { return taken.Load() == 2 }, 3*time.Second, 10*time.Millisecond)
}

const unsampledTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func TestTTSJobsSampleEveryJobOfAnUnsampledTrace(t *testing.T) {
	ctx := t.Context()
	app := serverlessTracingApp(t)
	jobs, err := bus.OpenTTSJobs(ctx, bustest.JetStream(t), 1)
	require.NoError(t, err)

	type handled struct {
		traceID string
		sampled bool
	}
	seen := make(chan handled, 1)
	stop, err := jobs.Consume(ctx, func(ctx context.Context, _ []byte) error {
		txn := newrelic.FromContext(ctx)
		seen <- handled{traceID: txn.GetTraceMetadata().TraceID, sampled: txn.IsSampled()}
		return nil
	}, 1, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	origin := app.StartTransaction("alert")
	t.Cleanup(origin.End)
	origin.AcceptDistributedTraceHeaders(newrelic.TransportQueue, http.Header{
		"Traceparent": {"00-" + unsampledTraceID + "-00f067aa0ba902b7-00"},
		"Tracestate":  {"123@nr=0-0-123-456-00f067aa0ba902b7-e8b91a159289ff74-0-0.100000-1795288712686"},
	})
	require.False(t, origin.IsSampled())
	require.NoError(t, jobs.Publish(newrelic.NewContext(ctx, origin), bus.TTSMessage{BroadcasterID: "42", EventID: "traced", Value: 1}))

	select {
	case got := <-seen:
		assert.Equal(t, handled{traceID: unsampledTraceID, sampled: true}, got)
	case <-time.After(3 * time.Second):
		t.Fatal("TTS job was not consumed")
	}
}

func TestTTSJobsLogFinalDeliveryAsAbandoned(t *testing.T) {
	ctx := t.Context()
	jobs, err := bus.OpenTTSJobs(ctx, bustest.JetStream(t), 1)
	require.NoError(t, err)
	observed, logs := observer.New(zap.WarnLevel)
	failure := bus.TTSRetryError{Delay: 10 * time.Millisecond, Err: errors.New("speech synthesis is unavailable right now")}
	stop, err := jobs.Consume(ctx, func(context.Context, []byte) error { return failure }, 1, zap.New(observed))
	require.NoError(t, err)
	t.Cleanup(stop)

	require.NoError(t, jobs.Publish(ctx, bus.TTSMessage{BroadcasterID: "42", EventID: "failing", Value: 1}))
	require.Eventually(t, func() bool {
		return logs.FilterMessage("TTS job abandoned after final delivery").Len() == 1
	}, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, 2, logs.FilterMessage("TTS job failed, retrying").Len())
	assert.Equal(t, uint64(3), logs.FilterMessage("TTS job abandoned after final delivery").All()[0].ContextMap()["delivery"])
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
