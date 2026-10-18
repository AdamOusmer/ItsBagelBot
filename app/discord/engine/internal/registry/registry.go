// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package registry

import (
	"fmt"

	"ItsBagelBot/app/discord/engine/module"
)

type Bound struct {
	Module  string
	Handler module.Handler
}

type Registry struct {
	events  map[string][]Bound
	slash   map[string]Bound
	buttons map[string]Bound
}

func New(mods ...module.Module) *Registry {
	r := &Registry{events: map[string][]Bound{}, slash: map[string]Bound{}, buttons: map[string]Bound{}}
	for _, m := range mods {
		r.add(m)
	}
	return r
}

func (r *Registry) add(m module.Module) {
	for t, h := range m.Events {
		r.events[t] = append(r.events[t], Bound{Module: m.Name, Handler: h})
	}
	for name, h := range m.Slash {
		claim(r.slash, "slash command", name, Bound{Module: m.Name, Handler: h})
	}
	for id, h := range m.Buttons {
		claim(r.buttons, "button", id, Bound{Module: m.Name, Handler: h})
	}
}

func claim(into map[string]Bound, kind, key string, b Bound) {
	if _, dup := into[key]; dup {
		panic(fmt.Sprintf("discord/engine/registry: duplicate %s %q claimed by module %q", kind, key, b.Module))
	}
	into[key] = b
}

func (r *Registry) Events(eventType string) []Bound { return r.events[eventType] }

func (r *Registry) Slash(name string) (Bound, bool) {
	h, ok := r.slash[name]
	return h, ok
}

func (r *Registry) Button(customID string) (Bound, bool) {
	h, ok := r.buttons[customID]
	return h, ok
}
