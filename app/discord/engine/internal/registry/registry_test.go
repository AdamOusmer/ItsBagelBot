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
	a := module.NewModule("a").On("GUILD_CREATE", noop).On("MESSAGE_CREATE", noop).Slash("ticket", noop).Button("x", noop).Build()
	b := module.NewModule("b").On("MESSAGE_CREATE", noop).Build()

	r := registry.New(a, b)

	require.Len(t, r.Events("GUILD_CREATE"), 1)
	require.Len(t, r.Events("MESSAGE_CREATE"), 2, "an event type can have several interested modules")
	_, slash := r.Slash("ticket")
	_, button := r.Button("x")
	_, unknown := r.Slash("nope")
	require.True(t, slash)
	require.True(t, button)
	require.False(t, unknown, "an unregistered slash name must not resolve")
}

func TestNewPanicsOnDuplicateClaimAcrossModules(t *testing.T) {
	cases := []struct {
		name string
		a, b module.Module
	}{
		{"slash command name", module.NewModule("a").Slash("ticket", noop).Build(), module.NewModule("b").Slash("ticket", noop).Build()},
		{"button custom id", module.NewModule("a").Button("x", noop).Build(), module.NewModule("b").Button("x", noop).Build()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Panics(t, func() { registry.New(tc.a, tc.b) })
		})
	}
}
