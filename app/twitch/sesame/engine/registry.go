// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

type BoundCommand struct {
	Cmd   module.Command
	Owner module.Module
}

type Registry struct {
	byEvent   map[string][]module.Module
	commands  map[string]BoundCommand
	needViews map[string]bool
}

func NewRegistry(log *zap.Logger, mods ...module.Module) *Registry {
	r := &Registry{
		byEvent:   make(map[string][]module.Module),
		commands:  make(map[string]BoundCommand),
		needViews: make(map[string]bool),
	}
	for _, m := range mods {
		r.indexEvents(m)
		r.indexCommands(log, m)
	}
	return r
}

func (r *Registry) indexEvents(m module.Module) {
	for evt := range m.Events {
		r.byEvent[evt] = append(r.byEvent[evt], m)
		if m.Kind != module.KindCore {
			r.needViews[evt] = true
		}
	}
}

func (r *Registry) indexCommands(log *zap.Logger, m module.Module) {
	for _, cmd := range m.Commands {
		r.bind(log, m, cmd, cmd.Name)
		for _, alias := range cmd.Aliases {
			r.bind(log, m, cmd, alias)
		}
		if m.Kind != module.KindCore {
			r.needViews[chatType] = true
		}
	}
}

func (r *Registry) bind(log *zap.Logger, m module.Module, cmd module.Command, trigger string) {
	if trigger == "" {
		return
	}
	if existing, dup := r.commands[trigger]; dup {
		if log != nil {
			log.Warn("engine: duplicate command trigger ignored",
				zap.String("trigger", trigger),
				zap.String("module", moduleLabel(m)),
				zap.String("kept_owner", moduleLabel(existing.Owner)),
			)
		}
		return
	}
	r.commands[trigger] = BoundCommand{Cmd: cmd, Owner: m}
}

func (r *Registry) For(eventType string) []module.Module { return r.byEvent[eventType] }

func (r *Registry) NeedsModuleViews(eventType string) bool { return r.needViews[eventType] }

func (r *Registry) Command(name string) (BoundCommand, bool) {
	bc, ok := r.commands[name]
	return bc, ok
}

func (r *Registry) ResolveCommand(name string) (bc BoundCommand, num string, ok bool) {
	if bc, ok := r.commands[name]; ok {
		return bc, "", true
	}
	base, digits := splitTrailingDigits(name)
	if digits == "" || base == "" {
		return BoundCommand{}, "", false
	}
	if bc, ok := r.commands[base]; ok && bc.Cmd.NumericSuffix {
		return bc, digits, true
	}
	return BoundCommand{}, "", false
}

func (r *Registry) Commands() map[string]BoundCommand { return r.commands }

func moduleLabel(m module.Module) string {
	if m.Name != "" {
		return m.Name
	}
	return "core"
}
