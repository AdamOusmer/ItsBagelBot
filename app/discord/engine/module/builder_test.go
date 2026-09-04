// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"context"
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"
)

func noopHandler(context.Context, *Context, Emit) error { return nil }

func TestBuildRegistersAllThreeAxes(t *testing.T) {
	b := NewModule("ticket")
	b.On("GUILD_CREATE", noopHandler)
	b.Slash("ticket", noopHandler)
	b.Button("bagel:ticket:open", noopHandler)

	m := b.Build()
	if m.Name != "ticket" {
		t.Fatalf("name = %q", m.Name)
	}
	if _, ok := m.Events["GUILD_CREATE"]; !ok {
		t.Fatal("missing event registration")
	}
	if _, ok := m.Slash["ticket"]; !ok {
		t.Fatal("missing slash registration")
	}
	if _, ok := m.Buttons["bagel:ticket:open"]; !ok {
		t.Fatal("missing button registration")
	}
}

func TestBuildPanicsOnEmptyName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty module name")
		}
	}()
	NewModule("").Build()
}

func TestOnKeepsLastHandlerOnDuplicateRegistration(t *testing.T) {
	calls := 0
	first := func(context.Context, *Context, Emit) error { calls += 1; return nil }
	second := func(context.Context, *Context, Emit) error { calls += 10; return nil }

	m := NewModule("x").On("T", first).On("T", second).Build()
	_ = m.Events["T"](context.Background(), &Context{}, func(ddiscord.Command) {})
	if calls != 10 {
		t.Fatalf("calls = %d, want 10 (second handler should win)", calls)
	}
}
