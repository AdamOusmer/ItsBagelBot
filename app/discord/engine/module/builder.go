// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"errors"
	"fmt"
)

// Builder is the fluent authoring surface for one module:
//
//	m := module.NewModule("welcome")
//	m.On("GUILD_MEMBER_ADD", onMemberAdd)
//	m.On("GUILD_MEMBER_REMOVE", onMemberRemove)
//	return m.Build()
//
// A Builder is single-use and not safe for concurrent use, matching sesame's.
type Builder struct {
	name    string
	events  map[string]Handler
	slash   map[string]Handler
	buttons map[string]Handler
}

// NewModule starts a module of the given name. Every module here is
// effectively sesame's KindCore (always on): Discord has exactly one module
// blob (ddiscord.Config), not a catalog of independently toggle-able
// sesame-style modules, so there is no Kind/Beta axis to carry over -- each
// Handler below reads the specific cfg.XOn() flag it cares about itself,
// exactly as community's handlers did before this split.
func NewModule(name string) *Builder {
	return &Builder{name: name}
}

// On registers the module's handler for one raw gateway dispatch type
// (GUILD_MEMBER_ADD, VOICE_STATE_UPDATE, MESSAGE_CREATE, ...). Registering
// the same type twice keeps the last handler.
func (b *Builder) On(eventType string, fn Handler) *Builder {
	if b.events == nil {
		b.events = make(map[string]Handler)
	}
	b.events[eventType] = fn
	return b
}

// Slash registers the module's handler for one slash-command name (no
// leading '/'). Only INTERACTION_CREATE events with a matching
// interaction.data.name reach it.
func (b *Builder) Slash(name string, fn Handler) *Builder {
	if b.slash == nil {
		b.slash = make(map[string]Handler)
	}
	b.slash[name] = fn
	return b
}

// Button registers the module's handler for one message-component custom id
// (see internal/discordapi's Custom* constants).
func (b *Builder) Button(customID string, fn Handler) *Builder {
	if b.buttons == nil {
		b.buttons = make(map[string]Handler)
	}
	b.buttons[customID] = fn
	return b
}

// Build validates the assembled module and returns its immutable form. It
// panics on a programmer error (empty name, a registration with a nil
// Handler): these are startup misconfigurations, not runtime data, so
// failing loud at boot is the right behavior, matching sesame's Build.
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

// Validate reports the first problem with the assembled module, or nil.
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
