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

const poweredUser uint64 = 21

func poweredOverlay(revision uint64, token string) *overlaysrpc.PoweredOverlay {
	return &overlaysrpc.PoweredOverlay{UserID: poweredUser, Token: token, Revision: revision, Config: overlaysrpc.DefaultPoweredConfig()}
}

func TestPoweredOverlayFoldKeepsTheNewestLink(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	p := NewProjector(Deps{Store: store, Log: zap.NewNop()})
	steps := []struct {
		name string
		dto  data.PoweredOverlayChangedDTO
		live string
		gone []string
	}{
		{"created", data.PoweredOverlayChangedDTO{UserID: poweredUser, Overlay: poweredOverlay(1, "first")}, "first", nil},
		{"rotated", data.PoweredOverlayChangedDTO{UserID: poweredUser, PreviousToken: "first", Overlay: poweredOverlay(2, "second")}, "second", []string{"first"}},
		{"older write redelivered", data.PoweredOverlayChangedDTO{UserID: poweredUser, Overlay: poweredOverlay(1, "first")}, "second", []string{"first"}},
		{"overlay without token dropped", data.PoweredOverlayChangedDTO{UserID: poweredUser, Overlay: poweredOverlay(3, "")}, "second", nil},
		{"deleted", data.PoweredOverlayChangedDTO{UserID: poweredUser, Deleted: true}, "", []string{"first", "second"}},
		{"last write redelivered after delete", data.PoweredOverlayChangedDTO{UserID: poweredUser, Overlay: poweredOverlay(2, "second")}, "", []string{"second"}},
		{"newer link after delete", data.PoweredOverlayChangedDTO{UserID: poweredUser, Overlay: poweredOverlay(3, "third")}, "third", []string{"second"}},
	}
	for _, step := range steps {
		require.NoError(t, p.HandlePoweredOverlayChanged(message(t, step.name, step.dto)), step.name)
		stored, _, err := store.PoweredOverlay(t.Context(), poweredUser)
		require.NoError(t, err, step.name)
		assert.Equal(t, step.live, stored.Token, step.name)
		assert.Equal(t, step.live, resolvedToken(t, store, step.live), step.name)
		for _, token := range step.gone {
			assert.Empty(t, resolvedToken(t, store, token), "%s: %s", step.name, token)
		}
	}
}

func resolvedToken(t *testing.T, store *projection.Store, token string) string {
	t.Helper()
	if token == "" {
		return ""
	}
	overlay, _, err := store.PoweredOverlayByToken(t.Context(), token)
	require.NoError(t, err)
	return overlay.Token
}

func TestPoweredOverlayRotationInvalidatesBothLinks(t *testing.T) {
	hub := bus.NewInvalidations(nil, "", zap.NewNop())
	var events []cache.Event
	hub.Watch(invalidate.ScopeOverlays, func(e cache.Event) { events = append(events, e) })
	p := NewProjector(Deps{Store: projection.NewStore(testValkey(t)), Invalidations: hub, Log: zap.NewNop()})

	require.NoError(t, p.HandlePoweredOverlayChanged(message(t, "rotate", data.PoweredOverlayChangedDTO{UserID: poweredUser, PreviousToken: "old", Overlay: poweredOverlay(4, "new")})))

	require.Len(t, events, 1)
	assert.Equal(t, cache.Event{Scope: invalidate.ScopeOverlays, ID: poweredUser, Keys: []string{"old", "new"}}, events[0])
}
