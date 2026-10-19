// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package registry

import (
	"fmt"
	"strings"

	"ItsBagelBot/app/discord/engine/module"
	catalog "ItsBagelBot/internal/domain/analytics"
)

type Registry struct {
	events  map[string][]module.Handler
	slash   map[string]module.Handler
	buttons map[string]module.Handler
}

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

func EventLabel(eventType string) string { return strings.ToLower(eventType) }

func (r *Registry) AnalyticsNames() map[catalog.Kind][]string {
	var names []string
	for t := range r.events {
		names = append(names, EventLabel(t))
	}
	for name := range r.slash {
		names = append(names, name)
	}
	for id := range r.buttons {
		names = append(names, id)
	}
	return map[catalog.Kind][]string{catalog.KindDiscord: names}
}

func (r *Registry) Events(eventType string) []module.Handler { return r.events[eventType] }

func (r *Registry) Slash(name string) (module.Handler, bool) {
	h, ok := r.slash[name]
	return h, ok
}

func (r *Registry) Button(customID string) (module.Handler, bool) {
	h, ok := r.buttons[customID]
	return h, ok
}
