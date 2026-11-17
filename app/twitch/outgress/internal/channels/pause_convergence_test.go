// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channels

import (
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/kvstate/kvtest"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDurablePauseReconcilesEveryReplicaWithoutPublish(t *testing.T) {
	kv := kvtest.New()
	_, err := kv.Create(t.Context(), "paused", []byte("false"))
	require.NoError(t, err)
	writer, peer := New(nil), New(nil)
	for _, r := range []*Registry{writer, peer} {
		t.Cleanup(r.Close)
		require.NoError(t, r.UseDurablePause(t.Context(), kv))
		require.NoError(t, r.StartPauseListener(testnats.Connect(t), "cache.invalidate", zap.NewNop()))
	}
	for _, paused := range []bool{true, false} {
		require.NoError(t, writer.SetPaused(t.Context(), paused))
		require.Eventually(t, func() bool {
			got, err := peer.Paused(t.Context())
			return err == nil && got == paused
		}, 3*time.Second, 10*time.Millisecond)
	}
}
