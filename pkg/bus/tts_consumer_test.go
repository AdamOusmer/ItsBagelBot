// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus_test

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
