// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"errors"
	"fmt"
)

type Builder struct {
	name    string
	events  map[string]Handler
	slash   map[string]Handler
	buttons map[string]Handler
}

func NewModule(name string) *Builder {
	return &Builder{name: name}
}

func (b *Builder) On(eventType string, fn Handler) *Builder {
	if b.events == nil {
		b.events = make(map[string]Handler)
	}
	b.events[eventType] = fn
	return b
}

func (b *Builder) Slash(name string, fn Handler) *Builder {
	if b.slash == nil {
		b.slash = make(map[string]Handler)
	}
	b.slash[name] = fn
	return b
}

func (b *Builder) Button(customID string, fn Handler) *Builder {
	if b.buttons == nil {
		b.buttons = make(map[string]Handler)
	}
	b.buttons[customID] = fn
	return b
}

func (b *Builder) Build() Module {
	if err := b.Validate(); err != nil {
		panic("discord/engine/module: " + err.Error())
	}
	return Module{
		Name:    b.name,
		Events:  copyHandlers(b.events),
		Slash:   copyHandlers(b.slash),
		Buttons: copyHandlers(b.buttons),
	}
}

func (b *Builder) Validate() error {
	if b.name == "" {
		return errors.New("module must have a non-empty name")
	}
	if err := checkHandlers("event", b.events); err != nil {
		return err
	}
	if err := checkHandlers("slash", b.slash); err != nil {
		return err
	}
	return checkHandlers("button", b.buttons)
}

func checkHandlers(kind string, m map[string]Handler) error {
	for key, fn := range m {
		if fn == nil {
			return fmt.Errorf("%s registration %q has a nil handler", kind, key)
		}
	}
	return nil
}

func copyHandlers(m map[string]Handler) map[string]Handler {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]Handler, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
