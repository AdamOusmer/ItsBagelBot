// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopRun(context.Context, *module.Context, string, module.Emit) error { return nil }
func noopEvt(context.Context, *module.Context, module.Emit) error         { return nil }

type commandSpec struct {
	Name          string
	Perm          module.Role
	Cooldown      time.Duration
	LiveOnly      bool
	AllowedUserID string
	Aliases       []string
	NumericSuffix bool
}

func specsOf(commands []module.Command) []commandSpec {
	specs := make([]commandSpec, len(commands))
	for i, c := range commands {
		specs[i] = commandSpec{c.Name, c.Perm, c.Cooldown, c.LiveOnly, c.AllowedUserID, c.Aliases, c.NumericSuffix}
	}
	return specs
}

func TestBuildAssemblesModuleCommandsAndEvents(t *testing.T) {
	m := module.NewModule("system", module.KindCore).Trial()
	m.Command("PiNg").Everyone().Run(noopRun)
	m.Command("sub").Sub().Run(noopRun)
	m.Command("vip").VIP().Run(noopRun)
	m.Command("announce").Mod().Run(noopRun)
	m.Command("lead").LeadMod().Run(noopRun)
	m.Command("owner").Broadcaster().Run(noopRun)
	m.Command("so").Mod().Cooldown(30*time.Second).LiveOnly().AllowUser("12345").Aliases("shoutout", "SO2").NumericSuffix().Run(noopRun)
	m.On("channel.chat.message", noopEvt)

	mod := m.Build()

	assert.Equal(t, "system", mod.Name)
	assert.Equal(t, module.KindCore, mod.Kind)
	assert.True(t, mod.Trial)
	assert.Contains(t, mod.Events, "channel.chat.message")
	assert.Equal(t, []commandSpec{
		{Name: "ping", Perm: module.RoleEveryone},
		{Name: "sub", Perm: module.RoleSubscriber},
		{Name: "vip", Perm: module.RoleVIP},
		{Name: "announce", Perm: module.RoleModerator},
		{Name: "lead", Perm: module.RoleLeadModerator},
		{Name: "owner", Perm: module.RoleBroadcaster},
		{
			Name: "so", Perm: module.RoleModerator, Cooldown: 30 * time.Second, LiveOnly: true,
			AllowedUserID: "12345", Aliases: []string{"shoutout", "so2"}, NumericSuffix: true,
		},
	}, specsOf(mod.Commands))
}

func TestBuildKeepsTheLastHandlerRegisteredForAnEvent(t *testing.T) {
	var last int
	first := func(context.Context, *module.Context, module.Emit) error { last = 1; return nil }
	second := func(context.Context, *module.Context, module.Emit) error { last = 2; return nil }

	m := module.NewModule("", module.KindCore)
	m.On("channel.chat.message", first)
	m.On("channel.chat.message", second)
	mod := m.Build()

	require.NoError(t, mod.Events["channel.chat.message"](context.Background(), &module.Context{}, func(*module.Output) {}))
	assert.Equal(t, 2, last)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		build   func() *module.Builder
		wantErr bool
	}{
		{"accepts a core module with no name", func() *module.Builder { return module.NewModule("", module.KindCore) }, false},
		{"accepts a named core module", func() *module.Builder { return module.NewModule("system", module.KindCore) }, false},
		{"accepts a named default module", func() *module.Builder {
			return module.NewModule("greeter", module.KindDefault).On("channel.chat.message", noopEvt)
		}, false},
		{"rejects a default module with no name", func() *module.Builder { return module.NewModule("", module.KindDefault) }, true},
		{"rejects an opt-in module with no name", func() *module.Builder { return module.NewModule("", module.KindOptIn) }, true},
		{"rejects an unknown kind", func() *module.Builder { return module.NewModule("x", module.Kind(99)) }, true},
		{"rejects a beta core module", func() *module.Builder { return module.NewModule("x", module.KindCore).Beta() }, true},
		{"accepts a beta opt-in module", func() *module.Builder { return module.NewModule("x", module.KindOptIn).Beta() }, false},
		{"accepts a trial core module", func() *module.Builder { return module.NewModule("trial", module.KindCore).Trial() }, false},
		{"rejects a trial opt-in module", func() *module.Builder { return module.NewModule("x", module.KindOptIn).Trial() }, true},
		{"rejects a trial default module", func() *module.Builder { return module.NewModule("x", module.KindDefault).Trial() }, true},
		{"rejects a command without Run", func() *module.Builder {
			m := module.NewModule("", module.KindCore)
			m.Command("ping")
			return m
		}, true},
		{"rejects a command with an empty name", func() *module.Builder {
			m := module.NewModule("", module.KindCore)
			m.Command("").Run(noopRun)
			return m
		}, true},
		{"rejects a duplicate command name", func() *module.Builder {
			m := module.NewModule("", module.KindCore)
			m.Command("ping").Run(noopRun)
			m.Command("ping").Run(noopRun)
			return m
		}, true},
		{"rejects an alias that collides with a command name", func() *module.Builder {
			m := module.NewModule("", module.KindCore)
			m.Command("ping").Run(noopRun)
			m.Command("pong").Aliases("ping").Run(noopRun)
			return m
		}, true},
		{"rejects an alias that collides with another alias", func() *module.Builder {
			m := module.NewModule("", module.KindCore)
			m.Command("a").Aliases("x").Run(noopRun)
			m.Command("b").Aliases("x").Run(noopRun)
			return m
		}, true},
		{"rejects an empty alias", func() *module.Builder {
			m := module.NewModule("", module.KindCore)
			m.Command("a").Aliases("").Run(noopRun)
			return m
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.build().Validate()

			assert.Equal(t, tt.wantErr, err != nil, "Validate() err=%v", err)
		})
	}
}

func TestBuildPanicsOnInvalidModule(t *testing.T) {
	assert.Panics(t, func() { module.NewModule("", module.KindDefault).Build() })
}
