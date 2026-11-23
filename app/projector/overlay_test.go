// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
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
	p := NewProjector(Deps{Store: store, NC: testnats.Connect(t), Log: zap.NewNop()})
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

func TestOverlayRotationInvalidatesBothLinks(t *testing.T) {
	hub := bus.NewInvalidations(nil, "", zap.NewNop())
	var events []cache.Event
	hub.Watch(invalidate.ScopeOverlays, func(e cache.Event) { events = append(events, e) })
	p := NewProjector(Deps{Store: projection.NewStore(testValkey(t)), NC: testnats.Connect(t), Invalidations: hub, Log: zap.NewNop()})

	change := data.OverlayChangedDTO{Overlay: cloudOverlay(4, "new"), PreviousTokenHash: overlaysrpc.HashToken("old")}
	require.NoError(t, p.HandleOverlayChanged(message(t, "rotate", change)))

	require.Len(t, events, 1)
	want := cache.Event{Scope: invalidate.ScopeOverlays, ID: cloudOverlayUser, Keys: []string{string(overlaysrpc.HashToken("new")), string(overlaysrpc.HashToken("old"))}}
	assert.Equal(t, want, events[0])
}

func TestOverlayFoldPublishesRevisionAndRevocation(t *testing.T) {
	nc := testnats.Connect(t)
	p := NewProjector(Deps{Store: projection.NewStore(testValkey(t)), NC: nc, Log: zap.NewNop()})
	seen := make(chan *nats.Msg, 8)
	sub, err := nc.ChanSubscribe("controls.overlay.>", seen)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	require.NoError(t, nc.Flush())

	steps := []struct {
		name    string
		dto     data.OverlayChangedDTO
		subject string
		payload string
	}{
		{"created", data.OverlayChangedDTO{Overlay: cloudOverlay(1, "first")}, overlaysrpc.OverlayRevisedSubject(5), "1"},
		{"saved", data.OverlayChangedDTO{Overlay: cloudOverlay(2, "first")}, overlaysrpc.OverlayRevisedSubject(5), "2"},
		{"rotated", data.OverlayChangedDTO{Overlay: cloudOverlay(3, "second"), PreviousTokenHash: overlaysrpc.HashToken("first")}, overlaysrpc.OverlayRevokedSubject(5), ""},
		{"deleted", data.OverlayChangedDTO{Overlay: cloudOverlay(3, "second"), Deleted: true}, overlaysrpc.OverlayRevokedSubject(5), ""},
	}
	for _, step := range steps {
		require.NoError(t, p.HandleOverlayChanged(message(t, step.name, step.dto)), step.name)
		select {
		case got := <-seen:
			assert.Equal(t, step.subject, got.Subject, step.name)
			assert.Equal(t, step.payload, string(got.Data), step.name)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: nothing published", step.name)
		}
	}

	require.NoError(t, p.HandleOverlayChanged(message(t, "older redelivered", data.OverlayChangedDTO{Overlay: cloudOverlay(1, "first")})))
	require.NoError(t, nc.Flush())
	assert.Empty(t, seen)
}
