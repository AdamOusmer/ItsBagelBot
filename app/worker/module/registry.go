package module

import "go.uber.org/zap"

// Registry indexes modules by the EventSub type they handle and bakes a flat
// index of every command any module owns. It is built once at startup and never
// mutated after, so lookups on the hot path are lock-free.
type Registry struct {
	byEvent  map[string][]Module
	commands map[string]Command
}

// NewRegistry builds the event-type index and the baked-command index from the
// given modules. A module is indexed under every type it returns from Events();
// each command it returns from Commands() is registered under its (lowercased)
// name. The first registration of a name wins: a duplicate is logged and
// ignored so a misconfigured module can never silently shadow another's command.
// log may be nil (duplicates are then silently ignored).
func NewRegistry(log *zap.Logger, modules ...Module) *Registry {
	r := &Registry{
		byEvent:  make(map[string][]Module),
		commands: make(map[string]Command),
	}
	for _, m := range modules {
		for _, evt := range m.Events() {
			r.byEvent[evt] = append(r.byEvent[evt], m)
		}
		for _, cmd := range m.Commands() {
			name := cmd.Name
			if name == "" {
				continue
			}
			if _, dup := r.commands[name]; dup {
				if log != nil {
					log.Warn("module: duplicate baked command ignored",
						zap.String("command", name),
						zap.String("module", m.Name()),
					)
				}
				continue
			}
			r.commands[name] = cmd
		}
	}
	return r
}

// For returns the modules registered for an event type, or nil when none are.
// The returned slice is owned by the registry and must not be mutated.
func (r *Registry) For(eventType string) []Module { return r.byEvent[eventType] }

// NeedsModuleViews reports whether any module for this event type is name-gated,
// i.e. whether the pipeline must fetch the broadcaster's ModuleView set before
// running them. Plain chat hits only core modules, so this returns false and the
// pipeline skips the projection read entirely.
func (r *Registry) NeedsModuleViews(eventType string) bool {
	for _, m := range r.byEvent[eventType] {
		if m.Name() != "" {
			return true
		}
	}
	return false
}

// Command looks up a baked command by its lowercased name.
func (r *Registry) Command(name string) (Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

// Commands returns the baked-command index. The map is owned by the registry and
// must be treated as read-only.
func (r *Registry) Commands() map[string]Command { return r.commands }
