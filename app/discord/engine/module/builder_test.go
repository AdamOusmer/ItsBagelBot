// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
)

func noopHandler(context.Context, *module.Context, module.Emit) error { return nil }

func TestBuildRegistersAllThreeAxes(t *testing.T) {
	m := module.NewModule(ddiscord.ModuleTickets).
		On("GUILD_CREATE", noopHandler).
		Slash("ticket", noopHandler).
		Button("bagel:ticket:open", noopHandler).
		Build()

	require.Equal(t, ddiscord.ModuleTickets, m.Name)
	require.Equal(t, ddiscord.ModuleDefault, m.Kind)
	require.Contains(t, m.Events, "GUILD_CREATE")
	require.Contains(t, m.Slash, "ticket")
	require.Contains(t, m.Buttons, "bagel:ticket:open")
}

func TestBuildRefusesAnInvalidModule(t *testing.T) {
	cases := []struct {
		name  string
		build func() module.Module
	}{
		{"empty name", func() module.Module { return module.NewModule("").Build() }},
		{"unknown module", func() module.Module { return module.NewModule("nope").Build() }},
		{"nil event handler", func() module.Module { return module.NewModule(ddiscord.ModuleWelcome).On("T", nil).Build() }},
		{"nil slash handler", func() module.Module { return module.NewModule(ddiscord.ModuleWelcome).Slash("s", nil).Build() }},
		{"nil button handler", func() module.Module { return module.NewModule(ddiscord.ModuleWelcome).Button("b", nil).Build() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Panics(t, func() { tc.build() })
		})
	}
}

func TestBuildDerivesKindFromTheModuleTable(t *testing.T) {
	require.Equal(t, ddiscord.ModuleOptIn, module.NewModule(ddiscord.ModuleLinkGuard).Build().Kind)
	require.Equal(t, ddiscord.ModuleCore, module.NewModule(ddiscord.ModuleIdentity).Build().Kind)
}

func TestContextReadsOwnSettingsAndPeers(t *testing.T) {
	c := &module.Context{
		Settings: []byte(`{"welcomeChannelId":"111"}`),
		Modules: ddiscord.GuildModules{Rows: map[string]ddiscord.ModuleRow{
			ddiscord.ModuleRoles: {Name: ddiscord.ModuleRoles, Enabled: true, Settings: []byte(`{"modsRoleId":"m1"}`)},
			ddiscord.ModuleLogs:  {Name: ddiscord.ModuleLogs, Enabled: false, Settings: []byte(`{"logChannelId":"222"}`)},
		}},
	}
	var own ddiscord.WelcomeSettings
	require.NoError(t, c.Decode(&own))
	require.Equal(t, "111", own.WelcomeChannelID)
	var logs ddiscord.LogsSettings
	require.NoError(t, c.Peer(ddiscord.ModuleLogs, &logs))
	require.Equal(t, "222", logs.LogChannelID)
	require.Equal(t, "m1", c.Roles().ModsRoleID)
	require.False(t, c.ModuleOn(ddiscord.ModuleLogs))
	require.True(t, c.ModuleOn(ddiscord.ModuleVoice), "absent default module is on")
	require.False(t, c.ModuleOn(ddiscord.ModuleLinkGuard), "absent opt-in module is off")

	var none ddiscord.WelcomeSettings
	require.NoError(t, (&module.Context{}).Decode(&none))
}

func TestOnKeepsLastHandlerOnDuplicateRegistration(t *testing.T) {
	calls := 0
	first := func(context.Context, *module.Context, module.Emit) error { calls += 1; return nil }
	second := func(context.Context, *module.Context, module.Emit) error { calls += 10; return nil }

	m := module.NewModule(ddiscord.ModuleWelcome).On("T", first).On("T", second).Build()
	require.NoError(t, m.Events["T"](context.Background(), &module.Context{}, func(ddiscord.Command) {}))

	require.Equal(t, 10, calls, "second handler should win")
}
