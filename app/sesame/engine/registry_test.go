package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/module"

	"github.com/stretchr/testify/assert"
)

func noRun(context.Context, *module.Context, string, module.Emit) error { return nil }
func noEvt(context.Context, *module.Context, module.Emit) error         { return nil }

// evtModule builds a module that handles the given event types (no commands).
func evtModule(name string, kind module.Kind, events ...string) module.Module {
	b := module.NewModule(name, kind)
	for _, e := range events {
		b.On(e, noEvt)
	}
	return b.Build()
}

// cmdModule builds a module owning one command with the given trigger and perm.
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

	// Plain chat hits only the core module: no ModuleView fetch needed.
	assert.False(t, reg.NeedsModuleViews(chatType))
	// stream.online has a name-gated module: the pipeline must fetch views.
	assert.True(t, reg.NeedsModuleViews("stream.online"))
	assert.False(t, reg.NeedsModuleViews("unmapped"))
}

func TestNeedsModuleViewsForNamedCommand(t *testing.T) {
	// A named module that owns a chat command forces the chat path to fetch views
	// so the command can be gated by the module's enable flag.
	named := cmdModule("extra", module.KindOptIn, "hi", module.RoleEveryone)
	reg := NewRegistry(nil, named)
	assert.True(t, reg.NeedsModuleViews(chatType))
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

	bc, ok = reg.Command("shoutout") // alias resolves to the same command
	assert.True(t, ok)
	assert.Equal(t, "so", bc.Cmd.Name)

	assert.Len(t, reg.Commands(), 2) // name + alias
}

func TestRegistryDuplicateCommandFirstWins(t *testing.T) {
	first := cmdModule("a", module.KindDefault, "dup", module.RoleEveryone)
	second := cmdModule("b", module.KindDefault, "dup", module.RoleBroadcaster)
	reg := NewRegistry(nil, first, second)

	bc, ok := reg.Command("dup")
	assert.True(t, ok)
	// First registration wins: the everyone perm and module "a" as owner.
	assert.Equal(t, module.RoleEveryone, bc.Cmd.Perm)
	assert.Equal(t, "a", bc.Owner.Name)
	assert.Len(t, reg.Commands(), 1)
}
