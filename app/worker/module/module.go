// Package module is the worker's pluggable behavior layer. Each feature is a
// standalone Module (the strategy): it declares the EventSub types it reacts to,
// reads its own enable-flag and config from the modules-service projection, runs
// its own permission and live-state checks, and returns the outgress messages to
// send. The pipeline owns a Registry of modules and runs the interested ones for
// each message in the consumer's own goroutine; there is no separate dispatcher.
//
// Two kinds of module exist:
//
//   - core modules (Name() == "") are always on and cannot be disabled by a
//     broadcaster: the custom-command processor and the live tracker. They are
//     the "default module system" the user cannot turn off.
//   - default/named modules (Name() != "") are gated by the broadcaster's
//     ModuleView from the modules service. A module may implement Defaulted to
//     declare its state when no row exists yet, so a default feature can ship on.
package module

import (
	"context"
	"encoding/json"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// Module is one pluggable behavior. Implementations must be idempotent: a
// message can be redelivered, so Handle may run more than once for the same
// event.
type Module interface {
	// Name is the module's key in the modules service (ModuleView.Name). An
	// empty name marks a core module that is always enabled and not gated by a
	// ModuleView row.
	Name() string
	// Events lists the EventSub types this module reacts to.
	Events() []string
	// Handle runs the module for one event and returns the outgress messages to
	// publish (nil/empty to do nothing).
	Handle(ctx context.Context, c *Context) ([]*outgress.Message, error)
}

// PremiumOnly is an optional capability: a module that implements it and returns
// true is skipped unless the event arrived on the premium regress.
type PremiumOnly interface {
	PremiumOnly() bool
}

// Defaulted is an optional capability for named modules: it declares whether the
// module is enabled when the broadcaster has no ModuleView row yet, so a default
// feature can ship enabled while staying toggleable.
type Defaulted interface {
	DefaultEnabled() bool
}

// Context is the per-message state the pipeline builds and hands to each module.
// It is confined to a single consumer goroutine, so its lazily-computed fields
// need no synchronization.
type Context struct {
	Env           lane.Envelope
	Regress       Regress
	BroadcasterID uint64
	Log           *zap.Logger

	// Config is this module's raw configuration blob from its ModuleView; the
	// pipeline sets it before calling Handle. Empty for core modules.
	Config json.RawMessage

	role    Role
	roleSet bool
}

// Chatter returns the chatter's resolved role, parsed once from the event badges
// and the broadcaster id. Non-chat events resolve to RoleEveryone.
func (c *Context) Chatter() Role {
	if !c.roleSet {
		c.role = ParseRole(c.Env)
		c.roleSet = true
	}
	return c.role
}

// Decode unmarshals the module's Config into out. A missing config is not an
// error: out is left at its zero value.
func (c *Context) Decode(out any) error {
	if len(c.Config) == 0 {
		return nil
	}
	return json.Unmarshal(c.Config, out)
}
