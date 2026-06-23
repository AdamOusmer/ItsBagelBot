// Package module is the worker's pluggable behavior layer. Each feature is a
// standalone Module (the strategy): it declares the EventSub types it reacts to,
// reads its own enable-flag and config from the modules-service projection, runs
// its own permission and live-state checks, and emits the outgress outputs to
// send. The pipeline owns a Registry of modules and runs the interested ones for
// each message in the consumer's own goroutine; there is no separate dispatcher.
//
// Two kinds of module exist:
//
//   - core modules (Name() == "") are always on and cannot be disabled by a
//     broadcaster: the command router and the live tracker. They are the
//     "default module system" the user cannot turn off.
//   - default/named modules (Name() != "") are gated by the broadcaster's
//     ModuleView from the modules service. A module may implement Defaulted to
//     declare its state when no row exists yet, so a default feature can ship on.
//
// Commands a module owns are declared via Commands(): the central command
// router (a core module) indexes them once at startup and dispatches every
// "!command" against that index plus the broadcaster's custom commands. A
// module that has no command-shaped behavior returns nil from Commands().
//
// The Handle path and the Command.Run path emit results through an Emit
// callback rather than returning a slice, so the pipeline can own pooling of the
// Output values: a module fills an *Output and hands it to emit, the pipeline
// serializes it synchronously and is free to recycle it. Modules must not retain
// the *Output (or the *Context) past the call.
package module

import (
	"context"
	"encoding/json"
	"time"

	"ItsBagelBot/internal/domain/event/lane"

	"go.uber.org/zap"
)

// Output is one outgress action a module wants to take, in the module layer's
// own minimal shape. The pipeline translates it onto an outgress.Message. It is
// pooled by the caller: a module fills it, hands it to Emit, and must not retain
// it afterwards.
//
//   - Type is the outgress message type (outgress.TypeChat, TypeAnnounce, ...).
//   - BroadcasterID is the target channel (the raw string id).
//   - Text is the message body.
//   - Color is the announce color (primary/blue/green/orange/purple); empty
//     unless Type is an announce.
//   - To is the shoutout target (login or id); empty unless Type is a shoutout.
type Output struct {
	Type          string
	BroadcasterID string
	Text          string
	Color         string
	To            string
}

// Emit publishes one Output. The callee must not retain o past the call: the
// pipeline may recycle it as soon as Emit returns.
type Emit func(o *Output)

// Module is one pluggable behavior. Implementations must be idempotent: a
// message can be redelivered, so Handle may run more than once for the same
// event.
type Module interface {
	// Name is the module's key in the modules service (ModuleView.Name). An
	// empty name marks a core module that is always enabled, immutable, and not
	// listed to broadcasters.
	Name() string
	// Events lists the EventSub types this module reacts to on its non-command
	// path. A module that only owns commands (and has no other event behavior)
	// may return nil here.
	Events() []string
	// Commands lists the baked commands this module owns. The command router
	// indexes them once at startup and dispatches them; the module itself never
	// parses "!command". Return nil if the module owns no commands.
	Commands() []Command
	// Handle runs the module's non-command event path for one event and emits
	// any outgress outputs. Command dispatch does not flow through here; it goes
	// through the router and each Command's Run.
	Handle(ctx context.Context, c *Context, emit Emit) error
}

// Command is one baked command owned by a module. The router gates and runs it
// centrally, so the same permission/live/cooldown semantics apply to baked and
// custom commands alike.
type Command struct {
	// Name is the lowercase trigger without the leading '!'.
	Name string
	// Perm is the minimum role required, unless AllowedUserID is set.
	Perm Role
	// Cooldown is the shared per-command window; zero means no cooldown.
	Cooldown time.Duration
	// LiveOnly gates the command to when the broadcaster is live.
	LiveOnly bool
	// AllowedUserID, when non-empty, restricts the command to exactly that
	// chatter id and overrides Perm entirely. Baked commands never set it; it
	// exists so the router can apply the same gate to custom commands.
	AllowedUserID string
	// Run executes the command after the gates pass, emitting any outputs. args
	// is the trimmed argument string after the command name.
	Run func(ctx context.Context, c *Context, args string, emit Emit) error
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
// need no synchronization. It is pooled (see pool.go): the pipeline gets one,
// fills it, runs the interested modules, then Resets and returns it. Modules
// must not retain it past the call.
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

// Reset zeroes the per-message fields so the Context can be reused from a pool.
// The struct itself stays usable; only the values are cleared. The Log pointer
// is left in place since the pipeline re-sets it per message anyway, but the
// lazy role cache is cleared so it never leaks across messages.
func (c *Context) Reset() {
	c.Env = lane.Envelope{}
	c.Regress = RegressStandard
	c.BroadcasterID = 0
	c.Config = nil
	c.role = RoleEveryone
	c.roleSet = false
}
