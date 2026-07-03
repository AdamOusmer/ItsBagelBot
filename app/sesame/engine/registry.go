package engine

import (
	"ItsBagelBot/app/sesame/module"

	"go.uber.org/zap"
)

// BoundCommand is a command together with the module that owns it. The pipeline
// gates the command through its owner's enable/premium state before running it,
// so a command on a disabled or premium-gated module never fires — commands and
// event handlers share one gating model. The owner also carries the module's
// config, which the pipeline wires into the Context so a command can read its
// module's settings.
type BoundCommand struct {
	Cmd   module.Command
	Owner module.Module
}

// Registry indexes the built modules by the EventSub type each handles on its
// non-command path, and bakes a flat index of every command any module owns
// (name and aliases alike), each bound to its owning module. It also precomputes
// which event types need the broadcaster's ModuleView set fetched. It is built
// once at startup and never mutated after, so lookups on the hot path are
// lock-free.
type Registry struct {
	byEvent   map[string][]module.Module
	commands  map[string]BoundCommand
	needViews map[string]bool
}

// NewRegistry builds the event-type index and the bound-command index from the
// given modules. A module is indexed under every type present in its Events map;
// each command it owns is registered under its name and each alias, bound to the
// module. The first registration of a trigger wins: a cross-module duplicate is
// logged and ignored so a misconfigured module can never silently shadow
// another's command. (Duplicates within a single module are already rejected by
// Builder.Build.) An event type — or the chat command path — that has any
// non-core module is flagged as needing ModuleView fetches. log may be nil
// (duplicates are then silently ignored).
func NewRegistry(log *zap.Logger, mods ...module.Module) *Registry {
	r := &Registry{
		byEvent:   make(map[string][]module.Module),
		commands:  make(map[string]BoundCommand),
		needViews: make(map[string]bool),
	}
	for _, m := range mods {
		for evt := range m.Events {
			r.byEvent[evt] = append(r.byEvent[evt], m)
			if m.Kind != module.KindCore {
				r.needViews[evt] = true
			}
		}
		for _, cmd := range m.Commands {
			r.bind(log, m, cmd, cmd.Name)
			for _, alias := range cmd.Aliases {
				r.bind(log, m, cmd, alias)
			}
			// A command owned by a named module must be gated by that module's
			// ModuleView, so the chat path needs the view fetch.
			if m.Kind != module.KindCore {
				r.needViews[chatType] = true
			}
		}
	}
	return r
}

// bind registers cmd (owned by m) under trigger, first-wins.
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

// For returns the modules registered for an event type, or nil when none are.
// The returned slice is owned by the registry and must not be mutated.
func (r *Registry) For(eventType string) []module.Module { return r.byEvent[eventType] }

// NeedsModuleViews reports whether processing this event type must fetch the
// broadcaster's ModuleView set: true when any event handler for the type, or any
// command owner on the chat path, is name-gated (not core). Plain chat with only
// core commands and handlers returns false, so the pipeline skips the projection
// read entirely.
func (r *Registry) NeedsModuleViews(eventType string) bool { return r.needViews[eventType] }

// Command looks up a bound command by a lowercased trigger (name or alias).
func (r *Registry) Command(name string) (BoundCommand, bool) {
	bc, ok := r.commands[name]
	return bc, ok
}

// Commands returns the bound-command index. The map is owned by the registry and
// must be treated as read-only.
func (r *Registry) Commands() map[string]BoundCommand { return r.commands }

// moduleLabel names a module for logs: its Name, or "core" for the unnamed core
// modules.
func moduleLabel(m module.Module) string {
	if m.Name != "" {
		return m.Name
	}
	return "core"
}
