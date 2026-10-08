// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package action_test

import (
	"context"
	"net/http"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/action"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nopRun(context.Context, *outgress.Message) error { return nil }

type route struct{ Method, Endpoint, As string }

func routeOf(m *outgress.Message) route { return route{m.Method, m.Endpoint, m.As} }

func buildRegistry() action.Registry {
	b := action.NewSet()
	b.Action("chat").Post("/helix/chat/messages").As(outgress.AsApp).Run(nopRun)
	b.Action("unban").Delete("/helix/moderation/bans").As(outgress.AsBot).Run(nopRun)
	b.Action("pin").Put("/helix/chat/pins").As(outgress.AsApp).Run(nopRun)
	b.Action("channel").Patch("/helix/channels").As(outgress.AsBroadcaster).Run(nopRun)
	b.Action("api").Passthrough().Run(nopRun)
	b.Action("eventsub").Internal().Run(nopRun)
	return b.Build()
}

func TestBuildProducesARegistryOfDeclaredActions(t *testing.T) {
	registry := buildRegistry()
	tests := []struct {
		name     string
		typ      string
		want     route
		wantKind action.Kind
		missing  bool
	}{
		{name: "post", typ: "chat", want: route{http.MethodPost, "/helix/chat/messages", outgress.AsApp}, wantKind: action.KindHelix},
		{name: "delete", typ: "unban", want: route{http.MethodDelete, "/helix/moderation/bans", outgress.AsBot}, wantKind: action.KindHelix},
		{name: "put", typ: "pin", want: route{http.MethodPut, "/helix/chat/pins", outgress.AsApp}, wantKind: action.KindHelix},
		{name: "patch", typ: "channel", want: route{http.MethodPatch, "/helix/channels", outgress.AsBroadcaster}, wantKind: action.KindHelix},
		{name: "passthrough", typ: "api", wantKind: action.KindPassthrough},
		{name: "internal", typ: "eventsub", wantKind: action.KindInternal},
		{name: "unknown type", typ: "unknown", missing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := registry.Lookup(tt.typ)

			require.Equal(t, !tt.missing, ok)
			if tt.missing {
				return
			}
			assert.Equal(t, tt.wantKind, got.Kind)
			assert.Equal(t, tt.want, route{got.Method, got.Endpoint, got.As})
		})
	}
}

func TestValidateRejectsMisdeclaredActions(t *testing.T) {
	tests := []struct {
		name    string
		declare func(*action.Builder)
		wantErr string
	}{
		{"empty type", func(b *action.Builder) { b.Action("").Post("/helix/x").Run(nopRun) }, "empty type"},
		{
			"duplicate type",
			func(b *action.Builder) {
				b.Action("chat").Post("/helix/chat/messages").Run(nopRun)
				b.Action("chat").Post("/helix/chat/messages").Run(nopRun)
			},
			"duplicate action type",
		},
		{"no route form", func(b *action.Builder) { b.Action("chat").Run(nopRun) }, "no route form"},
		{"two helix routes", func(b *action.Builder) { b.Action("chat").Post("/helix/a").Post("/helix/b").Run(nopRun) }, "more than one route form"},
		{"internal then helix route", func(b *action.Builder) { b.Action("chat").Internal().Post("/helix/a").Run(nopRun) }, "more than one route form"},
		{"helix route then passthrough", func(b *action.Builder) { b.Action("chat").Post("/helix/a").Passthrough().Run(nopRun) }, "more than one route form"},
		{"non-helix endpoint", func(b *action.Builder) { b.Action("chat").Post("/v5/chat").Run(nopRun) }, "invalid route"},
		{"unknown identity", func(b *action.Builder) { b.Action("chat").Post("/helix/chat/messages").As("nobody").Run(nopRun) }, "unknown identity"},
		{"identity on internal action", func(b *action.Builder) { b.Action("eventsub").Internal().As(outgress.AsApp).Run(nopRun) }, "must not carry a token identity"},
		{"missing run", func(b *action.Builder) { b.Action("chat").Post("/helix/chat/messages") }, "has no Run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := action.NewSet()
			tt.declare(b)

			assert.ErrorContains(t, b.Validate(), tt.wantErr)
		})
	}
}

func TestBuildPanicsOnInvalidSet(t *testing.T) {
	b := action.NewSet()
	b.Action("chat").Run(nopRun)

	assert.Panics(t, func() { b.Build() })
}

func TestFillRoute(t *testing.T) {
	registry := buildRegistry()
	tests := []struct {
		name     string
		typ      string
		message  outgress.Message
		wantFill bool
		want     route
	}{
		{
			name: "fills a helix route from the declaration", typ: "chat", message: outgress.Message{Type: "chat"},
			wantFill: true, want: route{http.MethodPost, "/helix/chat/messages", outgress.AsApp},
		},
		{
			name: "keeps explicit fields", typ: "chat",
			message:  outgress.Message{Type: "chat", Method: http.MethodPut, Endpoint: "/helix/other", As: outgress.AsBot},
			wantFill: true, want: route{http.MethodPut, "/helix/other", outgress.AsBot},
		},
		{name: "refuses a passthrough without an endpoint", typ: "api", message: outgress.Message{Type: "api"}},
		{
			name: "admits a passthrough with a full route", typ: "api",
			message:  outgress.Message{Type: "api", Method: http.MethodGet, Endpoint: "/helix/users"},
			wantFill: true, want: route{http.MethodGet, "/helix/users", ""},
		},
		{name: "admits an internal action", typ: "eventsub", message: outgress.Message{Type: "eventsub"}, wantFill: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act, ok := registry.Lookup(tt.typ)
			require.True(t, ok)

			assert.Equal(t, tt.wantFill, act.FillRoute(&tt.message))
			if tt.wantFill {
				assert.Equal(t, tt.want, routeOf(&tt.message))
			}
		})
	}
}
