package module

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Builder is the fluent authoring surface for one module. A module fn creates a
// Builder with NewModule, declares its commands and event handlers, then calls
// Build to get the immutable Module the engine consumes:
//
//	m := module.NewModule("", module.KindCore)
//	m.Command("ping").Everyone().Run(pingRun)
//	m.Command("announce").Mod().Run(announce(""))
//	m.On("channel.chat.message", bagelGreet)
//	return m.Build()
//
// The Builder holds *Command pointers while it is being assembled so the chained
// CmdBuilder setters mutate the command in place; Build copies them into the
// immutable Module. A Builder is single-use and not safe for concurrent use.
type Builder struct {
	name   string
	kind   Kind
	events map[string]EventHandler
	cmds   []*Command
}

// NewModule starts a module of the given name and kind. Deps are not passed
// here: a command's Run and an event handler capture whatever services they need
// by closure, which is what keeps this package free of runtime wiring. A core
// module (KindCore) must use an empty name; a named module (KindDefault /
// KindOptIn) must use a non-empty one. Build enforces this.
func NewModule(name string, kind Kind) *Builder {
	return &Builder{name: name, kind: kind}
}

// On registers the module's non-command handler for one EventSub type (e.g.
// "channel.chat.message", "channel.raid", "stream.online"). Registering the same
// type twice keeps the last handler.
func (b *Builder) On(eventType string, fn EventHandler) *Builder {
	if b.events == nil {
		b.events = make(map[string]EventHandler)
	}
	b.events[eventType] = fn
	return b
}

// Command starts a command with the given trigger, returning a CmdBuilder to
// chain its gates. The trigger is lowercased so it matches the engine's
// case-insensitive lookup. The command is not complete until Run is called; a
// CmdBuilder left without Run is reported by Build.
func (b *Builder) Command(name string) *CmdBuilder {
	c := &Command{Name: strings.ToLower(name), Perm: RoleEveryone}
	b.cmds = append(b.cmds, c)
	return &CmdBuilder{b: b, cmd: c}
}

// Build validates the assembled module and returns its immutable form. It panics
// on a programmer error (bad kind/name pairing, empty or duplicate trigger, a
// command with no Run): these are startup misconfigurations, not runtime data,
// so failing loud at boot is the right behavior. Use Validate to check without
// panicking.
func (b *Builder) Build() Module {
	if err := b.Validate(); err != nil {
		panic("sesame/module: " + err.Error())
	}

	cmds := make([]Command, len(b.cmds))
	for i, c := range b.cmds {
		cmds[i] = *c
	}

	var events map[string]EventHandler
	if len(b.events) > 0 {
		events = make(map[string]EventHandler, len(b.events))
		for k, v := range b.events {
			events[k] = v
		}
	}

	return Module{
		Name:     b.name,
		Kind:     b.kind,
		Events:   events,
		Commands: cmds,
	}
}

// Validate reports the first problem with the assembled module, or nil when it
// is well formed. Build calls it and panics on a non-nil result; tests and the
// future registry can call it directly. It checks the kind/name pairing and,
// across all of the module's commands, that every trigger (name or alias) is
// non-empty, unique within the module, and backed by a Run.
func (b *Builder) Validate() error {
	if err := b.validateKindName(); err != nil {
		return err
	}
	return b.validateCommands()
}

// validateKindName enforces the kind/name pairing: core modules have an empty
// name, named modules a non-empty one.
func (b *Builder) validateKindName() error {
	switch b.kind {
	case KindCore:
		if b.name != "" {
			return fmt.Errorf("core module must have an empty name, got %q", b.name)
		}
	case KindDefault, KindOptIn:
		if b.name == "" {
			return fmt.Errorf("%s module must have a non-empty name", b.kind)
		}
	default:
		return fmt.Errorf("unknown module kind %d", int(b.kind))
	}
	return nil
}

// validateCommands checks every command and that no trigger (name or alias) is
// claimed twice within the module.
func (b *Builder) validateCommands() error {
	// owner maps a trigger token to the command that first claimed it.
	owner := make(map[string]string, len(b.cmds))
	for _, c := range b.cmds {
		if err := validateCommand(owner, c); err != nil {
			return err
		}
	}
	return nil
}

// validateCommand checks one command's name and Run, then claims its name and
// each alias as a trigger.
func validateCommand(owner map[string]string, c *Command) error {
	if c.Name == "" {
		return errors.New("command with an empty name")
	}
	if c.Run == nil {
		return fmt.Errorf("command %q has no Run (chain .Run to finish it)", c.Name)
	}
	if err := claim(owner, c.Name, c); err != nil {
		return err
	}
	for _, a := range c.Aliases {
		if a == "" {
			return fmt.Errorf("command %q has an empty alias", c.Name)
		}
		if err := claim(owner, a, c); err != nil {
			return err
		}
	}
	return nil
}

// claim records trigger as owned by command c, or errors when it is already taken.
func claim(owner map[string]string, trigger string, c *Command) error {
	if prev, dup := owner[trigger]; dup {
		return fmt.Errorf("duplicate command trigger %q (command %q collides with %q)", trigger, c.Name, prev)
	}
	owner[trigger] = c.Name
	return nil
}

// CmdBuilder chains a single command's gates. Every setter returns the same
// CmdBuilder so they read as one line; Run is the terminal that finishes the
// command. The setters mutate the *Command that Command already appended to the
// parent Builder, so a command with no Run still exists as an incomplete entry
// that Validate catches.
type CmdBuilder struct {
	b   *Builder
	cmd *Command
}

// Everyone sets the command's minimum role to RoleEveryone (the default).
func (c *CmdBuilder) Everyone() *CmdBuilder { c.cmd.Perm = RoleEveryone; return c }

// Sub requires the chatter to be at least a subscriber.
func (c *CmdBuilder) Sub() *CmdBuilder { c.cmd.Perm = RoleSubscriber; return c }

// VIP requires the chatter to be at least a VIP.
func (c *CmdBuilder) VIP() *CmdBuilder { c.cmd.Perm = RoleVIP; return c }

// Mod requires the chatter to be at least a moderator (a lead moderator or the
// broadcaster also satisfy it).
func (c *CmdBuilder) Mod() *CmdBuilder { c.cmd.Perm = RoleModerator; return c }

// Broadcaster restricts the command to the channel owner.
func (c *CmdBuilder) Broadcaster() *CmdBuilder { c.cmd.Perm = RoleBroadcaster; return c }

// Cooldown sets the shared per-command window; zero (the default) means none.
func (c *CmdBuilder) Cooldown(d time.Duration) *CmdBuilder { c.cmd.Cooldown = d; return c }

// LiveOnly gates the command to when the broadcaster is live.
func (c *CmdBuilder) LiveOnly() *CmdBuilder { c.cmd.LiveOnly = true; return c }

// AllowUser restricts the command to exactly one chatter id, overriding the role
// gate entirely.
func (c *CmdBuilder) AllowUser(id string) *CmdBuilder { c.cmd.AllowedUserID = id; return c }

// Aliases adds extra triggers that resolve to this command. They are lowercased
// to match the engine's lookup. Duplicates are rejected by Validate.
func (c *CmdBuilder) Aliases(a ...string) *CmdBuilder {
	for _, alias := range a {
		c.cmd.Aliases = append(c.cmd.Aliases, strings.ToLower(alias))
	}
	return c
}

// Run sets the command's handler and finishes it. It is terminal: it returns
// nothing so a command declaration cannot accidentally continue past it.
func (c *CmdBuilder) Run(fn RunFunc) { c.cmd.Run = fn }
