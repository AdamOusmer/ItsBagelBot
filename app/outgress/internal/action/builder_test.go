package action

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
)

func nopRun(context.Context, *outgress.Message) error { return nil }

// route flattens an action's route triple for single-comparison assertions.
func route(a Action) [3]string { return [3]string{a.Method, a.Endpoint, a.As} }

func TestBuildProducesImmutableRegistry(t *testing.T) {
	b := NewSet()
	b.Action("chat").Post("/helix/chat/messages").As(outgress.AsApp).Run(nopRun)
	b.Action("unban").Delete("/helix/moderation/bans").As(outgress.AsBot).Run(nopRun)
	b.Action("pin").Put("/helix/chat/pins").As(outgress.AsApp).Run(nopRun)
	b.Action("api").Passthrough().Run(nopRun)
	b.Action("eventsub").Internal().Run(nopRun)
	registry := b.Build()

	chat, ok := registry.Lookup("chat")
	if !ok {
		t.Fatal("chat action missing")
	}
	if got, want := route(chat), [3]string{http.MethodPost, "/helix/chat/messages", outgress.AsApp}; got != want {
		t.Fatalf("chat route = %v, want %v", got, want)
	}
	if unban, _ := registry.Lookup("unban"); unban.Method != http.MethodDelete {
		t.Fatalf("unban method = %q, want DELETE", unban.Method)
	}
	if api, _ := registry.Lookup("api"); api.Kind != KindPassthrough {
		t.Fatalf("api kind = %v, want passthrough", api.Kind)
	}
	if es, _ := registry.Lookup("eventsub"); es.Kind != KindInternal {
		t.Fatalf("eventsub kind = %v, want internal", es.Kind)
	}
	if _, ok := registry.Lookup("unknown"); ok {
		t.Fatal("unknown type unexpectedly resolved")
	}
}

func TestValidateRejectsMisdeclaredActions(t *testing.T) {
	tests := []struct {
		name    string
		declare func(*Builder)
		wantErr string
	}{
		{
			name:    "empty type",
			declare: func(b *Builder) { b.Action("").Post("/helix/x").Run(nopRun) },
			wantErr: "empty type",
		},
		{
			name: "duplicate type",
			declare: func(b *Builder) {
				b.Action("chat").Post("/helix/chat/messages").Run(nopRun)
				b.Action("chat").Post("/helix/chat/messages").Run(nopRun)
			},
			wantErr: "duplicate action type",
		},
		{
			name:    "no route form",
			declare: func(b *Builder) { b.Action("chat").Run(nopRun) },
			wantErr: "no route form",
		},
		{
			name:    "two helix routes",
			declare: func(b *Builder) { b.Action("chat").Post("/helix/a").Post("/helix/b").Run(nopRun) },
			wantErr: "more than one route form",
		},
		{
			name:    "internal then helix route",
			declare: func(b *Builder) { b.Action("chat").Internal().Post("/helix/a").Run(nopRun) },
			wantErr: "more than one route form",
		},
		{
			name:    "helix route then passthrough",
			declare: func(b *Builder) { b.Action("chat").Post("/helix/a").Passthrough().Run(nopRun) },
			wantErr: "more than one route form",
		},
		{
			name:    "non-helix endpoint",
			declare: func(b *Builder) { b.Action("chat").Post("/v5/chat").Run(nopRun) },
			wantErr: "invalid route",
		},
		{
			name:    "unknown identity",
			declare: func(b *Builder) { b.Action("chat").Post("/helix/chat/messages").As("nobody").Run(nopRun) },
			wantErr: "unknown identity",
		},
		{
			name:    "identity on internal action",
			declare: func(b *Builder) { b.Action("eventsub").Internal().As(outgress.AsApp).Run(nopRun) },
			wantErr: "must not carry a token identity",
		},
		{
			name:    "missing run",
			declare: func(b *Builder) { b.Action("chat").Post("/helix/chat/messages") },
			wantErr: "has no Run",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewSet()
			tc.declare(b)
			err := b.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildPanicsOnInvalidSet(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Build did not panic on an invalid set")
		}
	}()
	b := NewSet()
	b.Action("chat").Run(nopRun)
	b.Build()
}

func TestFillRouteExplicitFieldsWin(t *testing.T) {
	b := NewSet()
	b.Action("chat").Post("/helix/chat/messages").As(outgress.AsApp).Run(nopRun)
	b.Action("api").Passthrough().Run(nopRun)
	b.Action("eventsub").Internal().Run(nopRun)
	registry := b.Build()

	chat, _ := registry.Lookup("chat")
	m := &outgress.Message{Type: "chat"}
	if !chat.FillRoute(m) {
		t.Fatal("chat route did not fill")
	}
	filled := [3]string{m.Method, m.Endpoint, m.As}
	if want := [3]string{http.MethodPost, "/helix/chat/messages", outgress.AsApp}; filled != want {
		t.Fatalf("filled message route = %v, want %v", filled, want)
	}

	explicit := &outgress.Message{Type: "chat", Method: http.MethodPut, Endpoint: "/helix/other", As: outgress.AsBot}
	if !chat.FillRoute(explicit) {
		t.Fatal("explicit route rejected")
	}
	kept := [3]string{explicit.Method, explicit.Endpoint, explicit.As}
	if want := [3]string{http.MethodPut, "/helix/other", outgress.AsBot}; kept != want {
		t.Fatalf("explicit fields overwritten: %v, want %v", kept, want)
	}

	api, _ := registry.Lookup("api")
	if api.FillRoute(&outgress.Message{Type: "api"}) {
		t.Fatal("passthrough without an endpoint unexpectedly admitted")
	}
	if !api.FillRoute(&outgress.Message{Type: "api", Method: http.MethodGet, Endpoint: "/helix/users"}) {
		t.Fatal("passthrough with a full route rejected")
	}

	es, _ := registry.Lookup("eventsub")
	if !es.FillRoute(&outgress.Message{Type: "eventsub"}) {
		t.Fatal("internal action rejected")
	}
}
