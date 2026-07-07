package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
)

// The automod module is the config surface for the inline gate: a named
// KindDefault module (dashboard toggle + config blob) whose chat registration is
// what makes the pipeline fetch its ModuleView. Pin the contract.
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
	// The handler is a pure no-op: the gate runs inline in the pipeline.
	if err := h(context.Background(), &module.Context{}, func(*module.Output) {
		t.Fatal("automod handler must never emit")
	}); err != nil {
		t.Fatalf("no-op handler errored: %v", err)
	}
}

// All() wires the automod module in, so a real registry marks chat as needing
// ModuleViews and the per-broadcaster row reaches the pipeline.
func TestAllIncludesAutomod(t *testing.T) {
	for _, m := range All(engine.Deps{}) {
		if m.Name == "automod" {
			return
		}
	}
	t.Fatal("All() must include the automod module")
}
