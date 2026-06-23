package module

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type stubModule struct {
	name     string
	events   []string
	commands []Command
}

func (s stubModule) Name() string        { return s.name }
func (s stubModule) Events() []string    { return s.events }
func (s stubModule) Commands() []Command { return s.commands }
func (s stubModule) Handle(context.Context, *Context, Emit) error {
	return nil
}

func TestRegistryRouting(t *testing.T) {
	core := stubModule{name: "", events: []string{"channel.chat.message"}}
	named := stubModule{name: "bagel", events: []string{"channel.chat.message", "stream.online"}}
	reg := NewRegistry(nil, core, named)

	chat := reg.For("channel.chat.message")
	assert.Len(t, chat, 2)
	assert.Len(t, reg.For("stream.online"), 1)
	assert.Empty(t, reg.For("channel.cheer"))
}

func TestNeedsModuleViews(t *testing.T) {
	core := stubModule{name: "", events: []string{"channel.chat.message"}}
	named := stubModule{name: "bagel", events: []string{"stream.online"}}
	reg := NewRegistry(nil, core, named)

	// Plain chat hits only the core module: no ModuleView fetch needed.
	assert.False(t, reg.NeedsModuleViews("channel.chat.message"))
	// stream.online has a name-gated module: the pipeline must fetch views.
	assert.True(t, reg.NeedsModuleViews("stream.online"))
	assert.False(t, reg.NeedsModuleViews("unmapped"))
}

func TestRegistryBakedCommandIndex(t *testing.T) {
	ping := Command{Name: "ping", Perm: RoleEveryone, Cooldown: 5 * time.Second}
	uptime := Command{Name: "uptime", Perm: RoleModerator, LiveOnly: true}
	m := stubModule{name: "", commands: []Command{ping, uptime}}
	reg := NewRegistry(nil, m)

	got, ok := reg.Command("ping")
	assert.True(t, ok)
	assert.Equal(t, "ping", got.Name)
	assert.Equal(t, 5*time.Second, got.Cooldown)

	got, ok = reg.Command("uptime")
	assert.True(t, ok)
	assert.True(t, got.LiveOnly)
	assert.Equal(t, RoleModerator, got.Perm)

	_, ok = reg.Command("nope")
	assert.False(t, ok)

	assert.Len(t, reg.Commands(), 2)
}

func TestRegistryDuplicateCommandFirstWins(t *testing.T) {
	first := stubModule{name: "a", commands: []Command{{Name: "dup", Perm: RoleEveryone}}}
	second := stubModule{name: "b", commands: []Command{{Name: "dup", Perm: RoleBroadcaster}}}
	reg := NewRegistry(nil, first, second)

	got, ok := reg.Command("dup")
	assert.True(t, ok)
	// First registration wins: the everyone perm, not the broadcaster one.
	assert.Equal(t, RoleEveryone, got.Perm)
	assert.Len(t, reg.Commands(), 1)
}
