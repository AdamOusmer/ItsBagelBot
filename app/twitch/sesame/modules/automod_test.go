// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
)

func TestAutomodModuleShape(t *testing.T) {
	m := Automod(engine.Deps{})

	if m.Name != "automod" {
		t.Fatalf("name = %q, want automod (the MODULE_CATALOG id)", m.Name)
	}
	if m.Kind != module.KindDefault {
		t.Fatalf("kind = %v, want KindDefault (ships enabled, toggleable)", m.Kind)
	}
	if len(m.Commands) != 0 {
		t.Fatalf("automod must own no commands, got %d", len(m.Commands))
	}

	h := m.Events["channel.chat.message"]
	if h == nil {
		t.Fatal("automod must register a chat handler: it forces the ModuleView fetch")
	}
	if err := h(context.Background(), &module.Context{}, func(*module.Output) {
		t.Fatal("automod handler must never emit")
	}); err != nil {
		t.Fatalf("no-op handler errored: %v", err)
	}
}

func TestAllIncludesAutomod(t *testing.T) {
	for _, m := range All(engine.Deps{}) {
		if m.Name == "automod" {
			return
		}
	}
	t.Fatal("All() must include the automod module")
}
