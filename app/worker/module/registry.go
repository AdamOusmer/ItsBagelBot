package module

// Registry indexes modules by the EventSub type they handle. It is built once at
// startup and never mutated after, so lookups on the hot path are lock-free.
type Registry struct {
	byEvent map[string][]Module
}

// NewRegistry builds the event-type index from the given modules. A module is
// indexed under every type it returns from Events().
func NewRegistry(modules ...Module) *Registry {
	r := &Registry{byEvent: make(map[string][]Module)}
	for _, m := range modules {
		for _, evt := range m.Events() {
			r.byEvent[evt] = append(r.byEvent[evt], m)
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
