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
		r.indexEvents(m)
		r.indexCommands(log, m)
	}
	return r
}

// indexEvents registers a module under every event type it handles, flagging the
// type as needing ModuleView fetches when the module is name-gated.
func (r *Registry) indexEvents(m module.Module) {
	for evt := range m.Events {
		r.byEvent[evt] = append(r.byEvent[evt], m)
		if m.Kind != module.KindCore {
			r.needViews[evt] = true
		}
	}
}

// indexCommands registers a module's commands (name and aliases) in the bound
// index. A command owned by a named module must be gated by that module's
// ModuleView, so it flags the chat path as needing the view fetch.
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

// ResolveCommand resolves a chat trigger to a bound command. It first tries an
// exact match; on a miss it strips a trailing run of digits and retries the
// base, matching only when that base command opted into a numeric suffix
// (Command.NumericSuffix). "clip30" resolves to "clip" with num="30"; an exact
// hit returns num="". The digits are returned for the caller's information but
// are not the command's argument string. This lets a built-in like !clip accept
// an inline number without registering every !clipN variant.
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
