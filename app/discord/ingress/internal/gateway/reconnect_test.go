// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReconnectBackoffGrowsCapsAndResetsAfterAStableSocket(t *testing.T) {
	const (
		failures    = 20
		stableLoops = 3
		socketLife  = 6 * time.Minute
		slack       = 10 * time.Millisecond
	)
	ceilings := append([]time.Duration{1, 2, 4, 8, 16, 32}, slices.Repeat([]time.Duration{60}, failures-6)...)
	synctest.Test(t, func(t *testing.T) {
		stable := gatewayFrames{}
		stable.hello = frame(t, packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 1_000_000})})
		stable.ready = newFrames(t).ready
		ctx, cancel := context.WithCancel(context.Background())
		var dialedAt []time.Time
		dial := func(context.Context, string) (Conn, error) {
			dialedAt = append(dialedAt, time.Now())
			n := len(dialedAt)
			switch {
			case n <= failures:
				return nil, errRefused
			case n <= failures+stableLoops:
				conn := script{reads: [][]byte{stable.hello, stable.ready}}.conn()
				time.AfterFunc(socketLife, func() { _ = conn.Close() })
				return conn, nil
			default:
				cancel()
				return nil, errRefused
			}
		}
		sess := Session{Token: "bot-token", Dial: dial, Handle: &recHandler{}, budget: noBudgetLimits()}

		require.ErrorIs(t, sess.Run(ctx), context.Canceled)

		gaps := make([]time.Duration, 0, len(dialedAt))
		for i := 1; i < len(dialedAt); i++ {
			gaps = append(gaps, dialedAt[i].Sub(dialedAt[i-1]))
		}
		failureGaps, stableGaps := gaps[:failures], gaps[failures+1:]
		for i, gap := range failureGaps {
			require.LessOrEqual(t, gap, ceilings[i]*time.Second+slack, "wait %d stays under the capped schedule", i)
		}
		require.GreaterOrEqual(t, sum(failureGaps), 30*time.Second, "the waits grow instead of staying at the first step")
		for i, gap := range stableGaps {
			require.LessOrEqual(t, gap-socketLife, time.Second+slack, "wait after stable socket %d restarts the schedule", i)
		}
	})
}

func sum(ds []time.Duration) time.Duration {
	var total time.Duration
	for _, d := range ds {
		total += d
	}
	return total
}

func noBudgetLimits() *connectBudget {
	return &connectBudget{now: time.Now, sched: budgetSchedule{ceiling: 1 << 30, window: connectWindow}}
}
