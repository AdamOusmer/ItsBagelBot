// Package module is sesame's module authoring surface. A feature is declared as
// one Module built by a fluent Builder: it names itself, declares its Kind
// (core, default, or opt-in), lists the commands it owns, and registers any
// non-command event handlers. The Builder produces an immutable Module value
// that the sesame engine (a later layer) indexes and runs.
//
// This package is intentionally standalone: it holds only the authoring value
// types and the Builder. It carries no runtime wiring (no pipeline, consumer,
// projection, or Valkey), so a module fn captures whatever services it needs by
// closure and this package never has to know about them. That keeps the
// authoring surface small, cheap to import, and unit-testable on its own.
package module

import (
	"context"
	"time"
)

// Kind classifies a module's enable semantics, replacing the worker's magic
// empty-name convention and its Defaulted optional interface with one explicit
// field.
type Kind int

const (
	// KindCore is always on, never listed to broadcasters, and immutable: it is
	// never toggled or configured, and the engine skips the ModuleView check for
	// it entirely (no projection fetch on its account). Its Name is optional —
	// empty for the classic unnamed built-ins, or set to give a named built-in an
	// identity (it still has no ModuleView key and no config).
	KindCore Kind = iota
	// KindDefault is a named module that ships enabled: it runs unless the
	// broadcaster's ModuleView disables it.
	KindDefault
	// KindOptIn is a named module that ships disabled: it runs only when the
	// broadcaster's ModuleView enables it.
	KindOptIn
)

// String renders the kind for logs and errors.
func (k Kind) String() string {
	switch k {
	case KindCore:
		return "core"
	case KindDefault:
		return "default"
	case KindOptIn:
		return "opt-in"
	default:
		return "unknown"
	}
}

// Output is one outgress action a module wants to take, in the module layer's
// own minimal shape. The engine translates it onto an outgress.Message. It is
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
// engine may recycle it as soon as Emit returns.
type Emit func(o *Output)

// RunFunc executes a command after its gates pass, emitting any outputs. args is
// the trimmed argument string after the command name.
type RunFunc func(ctx context.Context, c *Context, args string, emit Emit) error

// EventHandler runs a module's non-command path for one event of a registered
// type and emits any outputs. Command dispatch never flows through here.
type EventHandler func(ctx context.Context, c *Context, emit Emit) error

// Command is one baked command a module owns. The engine gates and runs it
// centrally, so the same permission/live/cooldown semantics apply to baked and
// custom commands alike. Build normalizes Name and Aliases to lowercase so they
// match the engine's case-insensitive lookup.
type Command struct {
	// Name is the lowercase trigger without the leading '!'.
	Name string
	// Aliases are extra lowercase triggers that resolve to this same command.
	Aliases []string
	// Perm is the minimum role required, unless AllowedUserID is set.
	Perm Role
	// Cooldown is the shared per-command window; zero means no cooldown.
	Cooldown time.Duration
	// LiveOnly gates the command to when the broadcaster is live.
	LiveOnly bool
	// AllowedUserID, when non-empty, restricts the command to exactly that
	// chatter id and overrides Perm entirely.
	AllowedUserID string
	// NumericSuffix lets the trigger absorb a trailing run of digits typed
	// inline (e.g. "!clip30" resolves to "clip"). The digits are not passed to
	// the command; they only widen what matches this trigger. Used by built-ins
	// like !clip that accept an inline number.
	NumericSuffix bool
	// Run executes the command after the gates pass.
	Run RunFunc
}

// Module is the immutable artifact Build returns. The sesame engine indexes it:
// each command goes into the flat command index, and each Events entry registers
// the module under that EventSub type for its non-command path.
//
// There is deliberately no premium/feature gate here: premium vs standard is a
// routing lane (it decides which outgress subject a reply rides), not a feature
// switch. Every feature is available on both lanes.
type Module struct {
	Name     string
	Kind     Kind
	Events   map[string]EventHandler
	Commands []Command
	// Triggers is reserved: trigger-word matchers will land here so ingress can
	// stop filtering to "!"-prefixed messages. Not populated yet.
	// Triggers []Trigger
}
