// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
)

func noRun(context.Context, *module.Context, string, module.Emit) error { return nil }
func noEvt(context.Context, *module.Context, module.Emit) error         { return nil }

func evtModule(name string, kind module.Kind, events ...string) module.Module {
	b := module.NewModule(name, kind)
	for _, e := range events {
		b.On(e, noEvt)
	}
	return b.Build()
}

func cmdModule(name string, kind module.Kind, trigger string, perm module.Role) module.Module {
	b := module.NewModule(name, kind)
	c := b.Command(trigger)
	switch perm {
	case module.RoleBroadcaster:
		c.Broadcaster()
	case module.RoleModerator:
		c.Mod()
	default:
		c.Everyone()
	}
	c.Run(noRun)
	return b.Build()
}

func TestRegistryRouting(t *testing.T) {
	core := evtModule("", module.KindCore, chatType)
	named := evtModule("bagel", module.KindDefault, chatType, "stream.online")
	reg := NewRegistry(nil, core, named)

	assert.Len(t, reg.For(chatType), 2)
	assert.Len(t, reg.For("stream.online"), 1)
	assert.Empty(t, reg.For("channel.cheer"))
}

func TestNeedsModuleViews(t *testing.T) {
	core := evtModule("", module.KindCore, chatType)
	named := evtModule("bagel", module.KindDefault, "stream.online")
	reg := NewRegistry(nil, core, named)

	assert.False(t, reg.NeedsModuleViews(chatType))
	assert.True(t, reg.NeedsModuleViews("stream.online"))
	assert.False(t, reg.NeedsModuleViews("unmapped"))
}

func TestNeedsModuleViewsForNamedCommand(t *testing.T) {
	named := cmdModule("extra", module.KindOptIn, "hi", module.RoleEveryone)
	reg := NewRegistry(nil, named)
	assert.True(t, reg.NeedsModuleViews(chatType))
}

func TestNamedCoreSkipsModuleViews(t *testing.T) {
	m := cmdModule("system", module.KindCore, "sys", module.RoleEveryone)
	reg := NewRegistry(nil, m)
	assert.False(t, reg.NeedsModuleViews(chatType))
}

func TestRegistryCommandIndex(t *testing.T) {
	b := module.NewModule("", module.KindCore)
	b.Command("ping").Everyone().Cooldown(5 * time.Second).Run(noRun)
	b.Command("uptime").Mod().LiveOnly().Run(noRun)
	reg := NewRegistry(nil, b.Build())

	bc, ok := reg.Command("ping")
	assert.True(t, ok)
	assert.Equal(t, "ping", bc.Cmd.Name)
	assert.Equal(t, 5*time.Second, bc.Cmd.Cooldown)

	bc, ok = reg.Command("uptime")
	assert.True(t, ok)
	assert.True(t, bc.Cmd.LiveOnly)
	assert.Equal(t, module.RoleModerator, bc.Cmd.Perm)

	_, ok = reg.Command("nope")
	assert.False(t, ok)

	assert.Len(t, reg.Commands(), 2)
}

func TestRegistryAliasesIndexed(t *testing.T) {
	b := module.NewModule("", module.KindCore)
	b.Command("so").Aliases("shoutout").Run(noRun)
	reg := NewRegistry(nil, b.Build())

	bc, ok := reg.Command("so")
	assert.True(t, ok)
	assert.Equal(t, "so", bc.Cmd.Name)

	bc, ok = reg.Command("shoutout")
	assert.True(t, ok)
	assert.Equal(t, "so", bc.Cmd.Name)

	assert.Len(t, reg.Commands(), 2)
}

func TestRegistryDuplicateCommandFirstWins(t *testing.T) {
	first := cmdModule("a", module.KindDefault, "dup", module.RoleEveryone)
	second := cmdModule("b", module.KindDefault, "dup", module.RoleBroadcaster)
	reg := NewRegistry(nil, first, second)

	bc, ok := reg.Command("dup")
	assert.True(t, ok)
	assert.Equal(t, module.RoleEveryone, bc.Cmd.Perm)
	assert.Equal(t, "a", bc.Owner.Name)
	assert.Len(t, reg.Commands(), 1)
}

func TestSplitTrailingDigits(t *testing.T) {
	cases := []struct{ in, base, digits string }{
		{"clip30", "clip", "30"},
		{"clip", "clip", ""},
		{"clip0", "clip", "0"},
		{"30", "", "30"},
		{"", "", ""},
	}
	for _, c := range cases {
		base, digits := splitTrailingDigits(c.in)
		assert.Equal(t, c.base, base, "base of %q", c.in)
		assert.Equal(t, c.digits, digits, "digits of %q", c.in)
	}
}

func TestResolveCommandNumericSuffix(t *testing.T) {
	b := module.NewModule("", module.KindCore)
	b.Command("clip").Everyone().NumericSuffix().Run(noRun)
	b.Command("ping").Everyone().Run(noRun)
	reg := NewRegistry(nil, b.Build())

	bc, num, ok := reg.ResolveCommand("clip")
	assert.True(t, ok)
	assert.Equal(t, "", num)
	assert.Equal(t, "clip", bc.Cmd.Name)

	bc, num, ok = reg.ResolveCommand("clip30")
	assert.True(t, ok)
	assert.Equal(t, "30", num)
	assert.Equal(t, "clip", bc.Cmd.Name)

	_, _, ok = reg.ResolveCommand("ping5")
	assert.False(t, ok)

	_, _, ok = reg.ResolveCommand("nope")
	assert.False(t, ok)
	_, _, ok = reg.ResolveCommand("30")
	assert.False(t, ok)
}
