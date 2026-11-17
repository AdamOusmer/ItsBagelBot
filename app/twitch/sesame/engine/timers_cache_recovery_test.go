// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
)

func TestTimerCachesRetryFailuresWithoutDeliveryWork(t *testing.T) {
	for _, cache := range []string{"flags", "rearm"} {
		t.Run(cache, func(t *testing.T) {
			config, err := codec.Marshal(timersConfig{Timers: []timerDef{{ID: "t1", Enabled: true, MinChatLines: 1, AllowOffline: true, Interval: 60}}})
			require.NoError(t, err)
			reader := &countingReader{fakeReader: fakeReader{modules: map[string]projection.ModuleView{timersModuleName: {IsEnabled: true, Configs: config}}}}
			f := newGateStoreFixture(t, reader)
			err = errors.New("temporary dependency failure")
			reader.modErr = err
			f.store.live = fakeLive{err: err}
			read := func() chatFlags {
				if cache == "rearm" {
					f.store.throttledRearm(t.Context(), f.bid)
					return chatFlags{}
				}
				return f.store.chatFlags(t.Context(), f.bid)
			}
			read()
			reader.modErr = nil
			f.store.live = fakeLive{live: true}
			before := reader.moduleReads
			flags := read()
			require.Greater(t, reader.moduleReads, before)
			if cache == "flags" {
				require.Equal(t, chatFlags{gated: true, offline: true}, flags)
			}
		})
	}
}

type deadlineTimerReader struct {
	fakeReader
	deadline time.Time
}

func (r *deadlineTimerReader) Module(ctx context.Context, _ uint64, _ string) (projection.ModuleView, bool, error) {
	r.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	return projection.ModuleView{}, false, ctx.Err()
}

func TestCountChatLineDetachesCancellationAndPreservesDeadline(t *testing.T) {
	reader := &deadlineTimerReader{}
	f := newGateStoreFixture(t, reader)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	deadline, _ := ctx.Deadline()
	cancel()
	f.store.CountChatLine(ctx, f.bid)
	require.WithinDuration(t, deadline, reader.deadline, time.Millisecond)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}
