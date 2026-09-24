// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Builder struct {
	name   string
	kind   Kind
	beta   bool
	events map[string]EventHandler
	cmds   []*Command
}

func NewModule(name string, kind Kind) *Builder {
	return &Builder{name: name, kind: kind}
}

func (b *Builder) Beta() *Builder {
	b.beta = true
	return b
}

func (b *Builder) On(eventType string, fn EventHandler) *Builder {
	if b.events == nil {
		b.events = make(map[string]EventHandler)
	}
	b.events[eventType] = fn
	return b
}

func (b *Builder) Command(name string) *CmdBuilder {
	c := &Command{Name: strings.ToLower(name), Perm: RoleEveryone}
	b.cmds = append(b.cmds, c)
	return &CmdBuilder{b: b, cmd: c}
}

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
		Beta:     b.beta,
		Events:   events,
		Commands: cmds,
	}
}

func (b *Builder) Validate() error {
	if err := b.validateKindName(); err != nil {
		return err
	}
	return b.validateCommands()
}

func (b *Builder) validateKindName() error {
	switch b.kind {
	case KindCore:
		if b.beta {
			return fmt.Errorf("core module %q cannot be beta: it has no ModuleView to gate", b.name)
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

func (b *Builder) validateCommands() error {
	claimed := make(map[string]struct{}, len(b.cmds))
	for _, c := range b.cmds {
		if err := validateCommand(claimed, c); err != nil {
			return err
		}
	}
	return nil
}

func validateCommand(claimed map[string]struct{}, c *Command) error {
	if c.Name == "" {
		return errors.New("command with an empty name")
	}
	if c.Run == nil {
		return fmt.Errorf("command %q has no Run (chain .Run to finish it)", c.Name)
	}
	if err := claim(claimed, c.Name, c); err != nil {
		return err
	}
	for _, a := range c.Aliases {
		if a == "" {
			return fmt.Errorf("command %q has an empty alias", c.Name)
		}
		if err := claim(claimed, a, c); err != nil {
			return err
		}
	}
	return nil
}

func claim(claimed map[string]struct{}, trigger string, c *Command) error {
	if _, dup := claimed[trigger]; dup {
		return fmt.Errorf("duplicate command trigger %q in module (command %q)", trigger, c.Name)
	}
	claimed[trigger] = struct{}{}
	return nil
}

type CmdBuilder struct {
	b   *Builder
	cmd *Command
}

func (c *CmdBuilder) Everyone() *CmdBuilder { c.cmd.Perm = RoleEveryone; return c }

func (c *CmdBuilder) Sub() *CmdBuilder { c.cmd.Perm = RoleSubscriber; return c }

func (c *CmdBuilder) VIP() *CmdBuilder { c.cmd.Perm = RoleVIP; return c }

func (c *CmdBuilder) Mod() *CmdBuilder { c.cmd.Perm = RoleModerator; return c }

func (c *CmdBuilder) LeadMod() *CmdBuilder { c.cmd.Perm = RoleLeadModerator; return c }

func (c *CmdBuilder) Broadcaster() *CmdBuilder { c.cmd.Perm = RoleBroadcaster; return c }

func (c *CmdBuilder) Cooldown(d time.Duration) *CmdBuilder { c.cmd.Cooldown = d; return c }

func (c *CmdBuilder) LiveOnly() *CmdBuilder { c.cmd.LiveOnly = true; return c }

func (c *CmdBuilder) NumericSuffix() *CmdBuilder { c.cmd.NumericSuffix = true; return c }

func (c *CmdBuilder) AllowUser(id string) *CmdBuilder { c.cmd.AllowedUserID = id; return c }

func (c *CmdBuilder) Aliases(a ...string) *CmdBuilder {
	for _, alias := range a {
		c.cmd.Aliases = append(c.cmd.Aliases, strings.ToLower(alias))
	}
	return c
}

func (c *CmdBuilder) Run(fn RunFunc) { c.cmd.Run = fn }
