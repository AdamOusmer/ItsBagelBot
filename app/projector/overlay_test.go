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

const cloudOverlayUser uint64 = 22

func cloudOverlay(revision uint64, token string) overlaysrpc.Overlay {
	return overlaysrpc.Overlay{
		ID: 5, UserID: cloudOverlayUser, Kind: "system", Template: "powered", Name: "Powered by Bagel",
		TokenHash: overlaysrpc.HashToken(token), Revision: revision,
	}
}

func TestOverlayFoldKeepsTheNewestLink(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	p := NewProjector(Deps{Store: store, Log: zap.NewNop()})
	steps := []struct {
		name string
		dto  data.OverlayChangedDTO
		live string
		gone []string
	}{
		{"created", data.OverlayChangedDTO{Overlay: cloudOverlay(1, "first")}, "first", nil},
		{"rotated", data.OverlayChangedDTO{Overlay: cloudOverlay(2, "second"), PreviousTokenHash: overlaysrpc.HashToken("first")}, "second", []string{"first"}},
		{"older write redelivered", data.OverlayChangedDTO{Overlay: cloudOverlay(1, "first")}, "second", []string{"first"}},
		{"invalid hash dropped", data.OverlayChangedDTO{Overlay: overlaysrpc.Overlay{ID: 5, UserID: cloudOverlayUser, Kind: "system", Revision: 9, TokenHash: "bad"}}, "second", nil},
		{"deleted", data.OverlayChangedDTO{Overlay: cloudOverlay(2, "second"), Deleted: true}, "", []string{"first", "second"}},
		{"last write redelivered after delete", data.OverlayChangedDTO{Overlay: cloudOverlay(2, "second")}, "", []string{"second"}},
		{"newer link after delete", data.OverlayChangedDTO{Overlay: cloudOverlay(3, "third")}, "third", []string{"second"}},
	}
	for _, step := range steps {
		require.NoError(t, p.HandleOverlayChanged(message(t, step.name, step.dto)), step.name)
		if step.live != "" {
			assert.Equal(t, uint64(5), resolvedOverlay(t, store, step.live).ID, step.name)
		}
		for _, token := range step.gone {
			assert.Zero(t, resolvedOverlay(t, store, token).ID, "%s: %s", step.name, token)
		}
	}
}

func resolvedOverlay(t *testing.T, store *projection.Store, token string) overlaysrpc.Overlay {
	t.Helper()
	overlay, _, err := store.OverlayByHash(t.Context(), overlaysrpc.HashToken(token))
	require.NoError(t, err)
	return overlay
}

func TestAlphaFlipInvalidatesTheOwnersLinksOnce(t *testing.T) {
	hub := bus.NewInvalidations(nil, "", zap.NewNop())
	var events []cache.Event
	hub.Watch(invalidate.ScopeOverlays, func(e cache.Event) { events = append(events, e) })
	p := NewProjector(Deps{Store: projection.NewStore(testValkey(t)), Invalidations: hub, Log: zap.NewNop()})
	require.NoError(t, p.HandleOverlayChanged(message(t, "created", data.OverlayChangedDTO{Overlay: cloudOverlay(1, "first")})))
	require.NoError(t, p.HandleOverlayChanged(message(t, "rotated", data.OverlayChangedDTO{Overlay: cloudOverlay(2, "second"), PreviousTokenHash: overlaysrpc.HashToken("first")})))
	events = nil

	for _, alpha := range []bool{true, true, false} {
		change := data.UserChangedDTO{UserID: cloudOverlayUser, Status: "paid", IsActive: true, Alpha: alpha}
		require.NoError(t, p.HandleUserChanged(message(t, "user-change", change)))
	}

	want := cache.Event{Scope: invalidate.ScopeOverlays, ID: cloudOverlayUser, Keys: []string{string(overlaysrpc.HashToken("second"))}}
	assert.Equal(t, []cache.Event{want, want}, events)
}

func TestOverlayRotationInvalidatesBothLinks(t *testing.T) {
	hub := bus.NewInvalidations(nil, "", zap.NewNop())
	var events []cache.Event
	hub.Watch(invalidate.ScopeOverlays, func(e cache.Event) { events = append(events, e) })
	p := NewProjector(Deps{Store: projection.NewStore(testValkey(t)), Invalidations: hub, Log: zap.NewNop()})

	change := data.OverlayChangedDTO{Overlay: cloudOverlay(4, "new"), PreviousTokenHash: overlaysrpc.HashToken("old")}
	require.NoError(t, p.HandleOverlayChanged(message(t, "rotate", change)))

	require.Len(t, events, 1)
	want := cache.Event{Scope: invalidate.ScopeOverlays, ID: cloudOverlayUser, Keys: []string{string(overlaysrpc.HashToken("new")), string(overlaysrpc.HashToken("old"))}}
	assert.Equal(t, want, events[0])
}
