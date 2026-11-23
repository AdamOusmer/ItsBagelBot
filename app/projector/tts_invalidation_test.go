// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTtsChangeInvalidatesGatewayCaches(t *testing.T) {
	hash := overlaysrpc.TtsTokenHash("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	cases := []struct {
		name string
		hash overlaysrpc.TtsTokenHash
		keys []string
	}{
		{"configured link", hash, []string{string(hash)}},
		{"unlinked", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hub := bus.NewInvalidations(nil, "", zap.NewNop())
			var events []cache.Event
			hub.Watch(invalidate.ScopeOverlays, func(e cache.Event) { events = append(events, e) })
			p := NewProjector(Deps{Store: projection.NewStore(testValkey(t)), Invalidations: hub, Log: zap.NewNop()})

			overlay := overlaysrpc.TtsOverlay{UserID: cloudOverlayUser, Settings: overlaysrpc.DefaultTtsSettings(), TokenHash: tc.hash, Generation: 2, Revision: 3, CreatedAt: 1}
			require.NoError(t, p.handleTtsChanged(message(t, tc.name, data.TtsOverlayChangedDTO{Overlay: overlay})))

			require.Len(t, events, 1)
			assert.Equal(t, cache.Event{Scope: invalidate.ScopeOverlays, ID: cloudOverlayUser, Keys: tc.keys}, events[0])
		})
	}
}
