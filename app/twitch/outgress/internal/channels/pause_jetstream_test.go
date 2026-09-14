// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channels

import (
	"context"
	"testing"

	"ItsBagelBot/pkg/kvstate/kvtest"
	"github.com/stretchr/testify/require"
)

func TestDurablePauseSurvivesValkeyLossAndRestart(t *testing.T) {
	ctx := context.Background()
	kv := kvtest.New()
	_, err := kv.Create(ctx, "paused", []byte("true"))
	require.NoError(t, err)
	r := New(nil) // Any accidental Valkey call panics.
	defer r.Close()
	require.NoError(t, r.UseDurablePause(ctx, kv))
	state, err := r.loadPauseSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, state.paused)
	require.NoError(t, r.SetPaused(ctx, false))
	restarted := New(nil)
	defer restarted.Close()
	require.NoError(t, restarted.UseDurablePause(ctx, kv))
	state, err = restarted.loadPauseSnapshot(ctx)
	require.NoError(t, err)
	require.False(t, state.paused)
	require.NoError(t, restarted.SetPaused(ctx, true))
	state, err = r.loadPauseSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, state.paused)
}

func TestDurablePauseRejectsMissingState(t *testing.T) {
	r := New(nil)
	defer r.Close()
	r.pauseStore = kvtest.New()
	_, err := r.loadPauseSnapshot(context.Background())
	require.Error(t, err)
}
