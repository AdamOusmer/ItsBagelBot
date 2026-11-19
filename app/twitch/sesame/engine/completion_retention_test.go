// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReporterExpiryReleasesTicketsAndPreservesAggregates(t *testing.T) {
	for _, tc := range []struct {
		backend string
		overlap bool
	}{
		{"analytics", false}, {"analytics", true}, {"DATA", false}, {"DATA", true},
	} {
		name := tc.backend + "/replacement"
		if tc.overlap {
			name = tc.backend + "/overlap"
		}
		t.Run(name, func(t *testing.T) { testReporterExpiry(t, tc.backend, tc.overlap) })
	}
}

func testReporterExpiry(t *testing.T, backend string, overlap bool) {
	b := newCompletionBroker(t, 120*time.Millisecond, 600*time.Millisecond)
	client := valkeytest.Spawn(t)
	recorder := analytics.New(client, zap.NewNop(), nil, analytics.WithoutTicker())
	t.Cleanup(recorder.Close)
	reporter := &LoyaltyReporter{pub: b.pub, log: zap.NewNop(), earn: map[earnKey]*earnAgg{}, bumps: map[counterAgg]*bumpAgg{}}
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: b.pub, Log: zap.NewNop(), Analytics: recorder, Stats: reporter}, NewRegistry(zap.NewNop(), silentCore()), Config{})
	t.Cleanup(p.Close)
	key := catalog.CounterKey(time.Now())
	if backend == "analytics" {
		require.NoError(t, client.Do(t.Context(), client.B().Set().Key(key).Value("blocked").Build()).Error())
	}
	b.pub.failAll.Store(backend == "DATA")
	var expired []*bus.Message
	for range 2 {
		b.publish(t)
		msg := b.receive(t)
		require.NoError(t, p.Process(msg))
		expired = append(expired, msg)
	}
	p.Finish(expiredCompletionContext(t))
	if overlap {
		time.Sleep(300 * time.Millisecond)
	} else {
		require.Eventually(t, func() bool { return p.completions.remaining() == 0 }, 2*time.Second, time.Millisecond)
	}
	b.publish(t)
	live := b.receive(t)
	require.NoError(t, p.Process(live))
	if overlap {
		require.Eventually(t, func() bool { return p.completions.remaining() == 1 }, time.Second, time.Millisecond)
	}
	b.pub.failAll.Store(false)
	if backend == "analytics" {
		require.NoError(t, client.Do(t.Context(), client.B().Del().Key(key).Build()).Error())
	}
	p.Finish(t.Context())
	awaitCompletion(t, live, true)
	for _, msg := range expired {
		assertPendingCompletion(t, msg)
	}
	count, err := client.Do(t.Context(), client.B().Hget().Key(key).Field("chat.messages").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
}
