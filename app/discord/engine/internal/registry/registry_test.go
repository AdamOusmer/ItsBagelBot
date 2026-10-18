// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package registry_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/module"

	"github.com/stretchr/testify/require"
)

func noop(context.Context, *module.Context, module.Emit) error { return nil }

func TestNewIndexesAllThreeAxesAcrossModules(t *testing.T) {
	a := module.NewModule("welcome").On("GUILD_CREATE", noop).On("MESSAGE_CREATE", noop).Slash("ticket", noop).Button("x", noop).Build()
	b := module.NewModule("voice").On("MESSAGE_CREATE", noop).Build()

	r := registry.New(a, b)

	require.Len(t, r.Events("GUILD_CREATE"), 1)
	require.Len(t, r.Events("MESSAGE_CREATE"), 2, "an event type can have several interested modules")
	slashOwner, slash := r.Slash("ticket")
	buttonOwner, button := r.Button("x")
	_, unknown := r.Slash("nope")
	require.True(t, slash)
	require.True(t, button)
	require.False(t, unknown, "an unregistered slash name must not resolve")
	require.Equal(t, "welcome", slashOwner.Module)
	require.Equal(t, "welcome", buttonOwner.Module)
	owners := []string{r.Events("MESSAGE_CREATE")[0].Module, r.Events("MESSAGE_CREATE")[1].Module}
	require.ElementsMatch(t, []string{"welcome", "voice"}, owners)
}

func TestNewPanicsOnDuplicateClaimAcrossModules(t *testing.T) {
	cases := []struct {
		name string
		a, b module.Module
	}{
		{"slash command name", module.NewModule("welcome").Slash("ticket", noop).Build(), module.NewModule("voice").Slash("ticket", noop).Build()},
		{"button custom id", module.NewModule("welcome").Button("x", noop).Build(), module.NewModule("voice").Button("x", noop).Build()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Panics(t, func() { registry.New(tc.a, tc.b) })
		})
	}
}
