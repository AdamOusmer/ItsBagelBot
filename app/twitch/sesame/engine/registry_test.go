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

func noEvt(context.Context, *module.Context, module.Emit) error { return nil }

func eventModule(name string, kind module.Kind, events ...string) module.Module {
	b := module.NewModule(name, kind)
	for _, e := range events {
		b.On(e, noEvt)
	}
	return b.Build()
}

func commandModule(name string, kind module.Kind, trigger string, perm module.Role) module.Module {
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

func TestRegistryRoutesEventsAndKnowsWhenViewsAreNeeded(t *testing.T) {
	cases := []struct {
		name         string
		modules      []module.Module
		event        string
		wantHandlers int
		wantViews    bool
	}{
		{
			name:    "core and named handlers both receive chat and the named one needs views",
			modules: []module.Module{eventModule("", module.KindCore, chatType), eventModule("bagel", module.KindDefault, chatType, "stream.online")},
			event:   chatType, wantHandlers: 2, wantViews: true,
		},
		{
			name:    "chat stays view-free while only core handles it",
			modules: []module.Module{eventModule("", module.KindCore, chatType), eventModule("bagel", module.KindDefault, "stream.online")},
			event:   chatType, wantHandlers: 1,
		},
		{
			name:    "an event only the named module handles routes to it and needs views",
			modules: []module.Module{eventModule("", module.KindCore, chatType), eventModule("bagel", module.KindDefault, chatType, "stream.online")},
			event:   "stream.online", wantHandlers: 1, wantViews: true,
		},
		{
			name:    "an unmapped event routes nowhere and needs nothing",
			modules: []module.Module{eventModule("", module.KindCore, chatType), eventModule("bagel", module.KindDefault, "stream.online")},
			event:   "channel.cheer",
		},
		{
			name:    "a named command owner makes chat need module views",
			modules: []module.Module{commandModule("extra", module.KindOptIn, "hi", module.RoleEveryone)},
			event:   chatType, wantViews: true,
		},
		{
			name:    "a named core command owner does not",
			modules: []module.Module{commandModule("system", module.KindCore, "sys", module.RoleEveryone)},
			event:   chatType,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry(nil, tc.modules...)

			assert.Len(t, reg.For(tc.event), tc.wantHandlers)
			assert.Equal(t, tc.wantViews, reg.NeedsModuleViews(tc.event))
		})
	}
}

func commandRegistry() *Registry {
	b := module.NewModule("", module.KindCore)
	b.Command("ping").Everyone().Cooldown(5 * time.Second).Run(noRun)
	b.Command("uptime").Mod().LiveOnly().Run(noRun)
	b.Command("so").Aliases("shoutout").Run(noRun)
	b.Command("clip").Everyone().NumericSuffix().Run(noRun)
	return NewRegistry(nil,
		b.Build(),
		commandModule("a", module.KindDefault, "dup", module.RoleEveryone),
		commandModule("b", module.KindDefault, "dup", module.RoleBroadcaster),
	)
}

func TestRegistryIndexesCommandsAndAliases(t *testing.T) {
	type indexed struct {
		found    bool
		name     string
		owner    string
		cooldown time.Duration
		liveOnly bool
		perm     module.Role
	}
	cases := []struct {
		name    string
		command string
		want    indexed
	}{
		{"a command carries its cooldown", "ping", indexed{true, "ping", "", 5 * time.Second, false, module.RoleEveryone}},
		{"a command carries its live-only flag and permission", "uptime", indexed{true, "uptime", "", 0, true, module.RoleModerator}},
		{"an alias resolves to its command", "shoutout", indexed{true, "so", "", 0, false, module.RoleEveryone}},
		{"a duplicate trigger keeps the first owner", "dup", indexed{true, "dup", "a", 0, false, module.RoleEveryone}},
		{"an unknown trigger is not found", "nope", indexed{}},
	}
	reg := commandRegistry()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bc, ok := reg.Command(tc.command)

			assert.Equal(t, tc.want, indexed{ok, bc.Cmd.Name, bc.Owner.Name, bc.Cmd.Cooldown, bc.Cmd.LiveOnly, bc.Cmd.Perm})
		})
	}
	assert.Len(t, reg.Commands(), 6, "every trigger and alias is indexed once")
}

func TestRegistryResolveCommandSplitsNumericSuffixes(t *testing.T) {
	cases := []struct {
		name     string
		command  string
		wantName string
		wantNum  string
		wantOK   bool
	}{
		{"a bare suffix command resolves without a number", "clip", "clip", "", true},
		{"a numeric suffix is split off", "clip30", "clip", "30", true},
		{"a zero suffix is still a number", "clip0", "clip", "0", true},
		{"a command without suffix support rejects a number", "ping5", "", "", false},
		{"an unknown command is not found", "nope", "", "", false},
		{"a bare number is not a command", "30", "", "", false},
	}
	reg := commandRegistry()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bc, num, ok := reg.ResolveCommand(tc.command)

			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantName, bc.Cmd.Name)
			assert.Equal(t, tc.wantNum, num)
		})
	}
}
