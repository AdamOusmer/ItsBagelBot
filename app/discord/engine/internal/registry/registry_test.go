// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package registry

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/engine/module"
)

func noop(context.Context, *module.Context, module.Emit) error { return nil }

func TestNewIndexesAllThreeAxesAcrossModules(t *testing.T) {
	a := module.NewModule("a").On("GUILD_CREATE", noop).Slash("ticket", noop).Button("x", noop).Build()
	b := module.NewModule("b").On("MESSAGE_CREATE", noop).Build()

	r := New(a, b)
	if len(r.Events("GUILD_CREATE")) != 1 {
		t.Fatal("missing event handler from module a")
	}
	if len(r.Events("MESSAGE_CREATE")) != 1 {
		t.Fatal("missing event handler from module b")
	}
	if _, ok := r.Slash("ticket"); !ok {
		t.Fatal("missing slash handler")
	}
	if _, ok := r.Button("x"); !ok {
		t.Fatal("missing button handler")
	}
	if _, ok := r.Slash("nope"); ok {
		t.Fatal("unregistered slash name must not resolve")
	}
}

func TestEventTypeCanHaveSeveralInterestedModules(t *testing.T) {
	a := module.NewModule("a").On("MESSAGE_CREATE", noop).Build()
	b := module.NewModule("b").On("MESSAGE_CREATE", noop).Build()
	r := New(a, b)
	if got := len(r.Events("MESSAGE_CREATE")); got != 2 {
		t.Fatalf("handlers for MESSAGE_CREATE = %d, want 2", got)
	}
}

func TestNewPanicsOnDuplicateSlashAcrossModules(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic on a duplicate slash-command name")
		}
	}()
	a := module.NewModule("a").Slash("ticket", noop).Build()
	b := module.NewModule("b").Slash("ticket", noop).Build()
	New(a, b)
}

func TestNewPanicsOnDuplicateButtonAcrossModules(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic on a duplicate button custom id")
		}
	}()
	a := module.NewModule("a").Button("x", noop).Build()
	b := module.NewModule("b").Button("x", noop).Build()
	New(a, b)
}
