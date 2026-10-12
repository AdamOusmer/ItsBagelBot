// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"slices"

	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

type BoundCommand struct {
	Cmd   module.Command
	Owner module.Module
}

type Registry struct {
	variableModules map[string]module.Module
	byName          map[string]module.Module
	byEvent         map[string][]module.Module
	commands        map[string][]BoundCommand
	needViews       map[string]bool
}

func NewRegistry(log *zap.Logger, mods ...module.Module) *Registry {
	r := &Registry{
		variableModules: make(map[string]module.Module),
		byName:          make(map[string]module.Module),
		byEvent:         make(map[string][]module.Module),
		commands:        make(map[string][]BoundCommand),
		needViews:       make(map[string]bool),
	}
	for _, m := range mods {
		if m.Name != "" {
			r.byName[m.Name] = m
		}
		if len(m.Variables) > 0 {
			r.variableModules[m.Name] = m
		}
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
	owners := r.commands[trigger]
	if i := slices.IndexFunc(owners, alwaysClaims); i >= 0 && log != nil {
		log.Warn("engine: shadowed command trigger is unreachable",
			zap.String("trigger", trigger),
			zap.String("module", moduleLabel(m)),
			zap.String("kept_owner", moduleLabel(owners[i].Owner)),
		)
	}
	r.commands[trigger] = append(owners, BoundCommand{Cmd: cmd, Owner: m})
}

func alwaysClaims(bc BoundCommand) bool {
	return bc.Owner.Kind == module.KindCore && !bc.Owner.Trial && bc.Cmd.When == nil
}

func (r *Registry) For(eventType string) []module.Module { return r.byEvent[eventType] }

func (r *Registry) NeedsModuleViews(eventType string) bool { return r.needViews[eventType] }

func (r *Registry) Command(name string) (BoundCommand, bool) {
	owners := r.commands[name]
	if len(owners) == 0 {
		return BoundCommand{}, false
	}
	return owners[0], true
}

func (r *Registry) ResolveCommand(name string) (bc BoundCommand, num string, ok bool) {
	owners, num := r.resolveOwners(name)
	if len(owners) == 0 {
		return BoundCommand{}, "", false
	}
	return owners[0], num, true
}

func (r *Registry) resolveOwners(name string) (owners []BoundCommand, num string) {
	if owners := r.commands[name]; len(owners) > 0 {
		return owners, ""
	}
	base, digits := splitTrailingDigits(name)
	if digits == "" || base == "" {
		return nil, ""
	}
	if owners := r.commands[base]; len(owners) > 0 && owners[0].Cmd.NumericSuffix {
		return owners, digits
	}
	return nil, ""
}

func (r *Registry) Commands() map[string][]BoundCommand { return r.commands }

func moduleLabel(m module.Module) string {
	if m.Name != "" {
		return m.Name
	}
	return "core"
}
