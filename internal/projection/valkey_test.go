// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"testing"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func TestHydrationStateComplete(t *testing.T) {
	require.False(t, (HydrationState{User: true, Modules: true}).Complete())
	require.True(t, (HydrationState{User: true, Modules: true, Commands: true}).Complete())
}

func TestNewStorePinsTheAliasRetirementRead(t *testing.T) {
	store := NewStore(nil)
	require.True(t, pkg_valkey.IsPrimary(store.primary))
	require.False(t, pkg_valkey.IsPrimary(store.client), "ordinary projection reads stay node-local")
}
