// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package registry indexes the assembled module.Module set for dispatch,
// mirroring app/sesame/engine's Registry.
package registry

import (
	"fmt"

	"ItsBagelBot/app/discord/engine/module"
)

// Registry is the dispatcher's read-only index: a raw gateway event type can
// have several interested modules (each independently gated by its own
// Handler body), but a slash-command name or a button's custom id can only
// ever belong to one -- Discord itself enforces the former (registering the
// same slash name twice is a client-visible catalog conflict) and the
// latter is this bot's own naming discipline, so a duplicate here is a
// programmer error caught at boot, not a runtime ambiguity to resolve.
type Registry struct {
	events  map[string][]module.Handler
	slash   map[string]module.Handler
	buttons map[string]module.Handler
}

// New builds a Registry from the assembled modules. It panics on a
// duplicate slash or button registration across modules, matching Build's
// own "fail loud at boot" discipline.
func New(mods ...module.Module) *Registry {
	r := &Registry{events: map[string][]module.Handler{}, slash: map[string]module.Handler{}, buttons: map[string]module.Handler{}}
	for _, m := range mods {
		r.add(m)
	}
	return r
}

func (r *Registry) add(m module.Module) {
	for t, h := range m.Events {
		r.events[t] = append(r.events[t], h)
	}
	for name, h := range m.Slash {
		claim(r.slash, "slash command", name, m.Name, h)
	}
	for id, h := range m.Buttons {
		claim(r.buttons, "button", id, m.Name, h)
	}
}

func claim(into map[string]module.Handler, kind, key, moduleName string, h module.Handler) {
	if _, dup := into[key]; dup {
		panic(fmt.Sprintf("discord/engine/registry: duplicate %s %q claimed by module %q", kind, key, moduleName))
	}
	into[key] = h
}

// Events returns every handler registered for a raw gateway event type.
func (r *Registry) Events(eventType string) []module.Handler { return r.events[eventType] }

// Slash returns the handler registered for a slash-command name, if any.
func (r *Registry) Slash(name string) (module.Handler, bool) {
	h, ok := r.slash[name]
	return h, ok
}

// Button returns the handler registered for a message-component custom id,
// if any.
func (r *Registry) Button(customID string) (module.Handler, bool) {
	h, ok := r.buttons[customID]
	return h, ok
}
