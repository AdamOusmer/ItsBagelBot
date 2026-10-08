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
	m := module.NewModule("ticket").
		On("GUILD_CREATE", noopHandler).
		Slash("ticket", noopHandler).
		Button("bagel:ticket:open", noopHandler).
		Build()

	require.Equal(t, "ticket", m.Name)
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
		{"nil event handler", func() module.Module { return module.NewModule("x").On("T", nil).Build() }},
		{"nil slash handler", func() module.Module { return module.NewModule("x").Slash("s", nil).Build() }},
		{"nil button handler", func() module.Module { return module.NewModule("x").Button("b", nil).Build() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Panics(t, func() { tc.build() })
		})
	}
}

func TestOnKeepsLastHandlerOnDuplicateRegistration(t *testing.T) {
	calls := 0
	first := func(context.Context, *module.Context, module.Emit) error { calls += 1; return nil }
	second := func(context.Context, *module.Context, module.Emit) error { calls += 10; return nil }

	m := module.NewModule("x").On("T", first).On("T", second).Build()
	require.NoError(t, m.Events["T"](context.Background(), &module.Context{}, func(ddiscord.Command) {}))

	require.Equal(t, 10, calls, "second handler should win")
}
