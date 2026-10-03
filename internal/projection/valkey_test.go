// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"testing"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
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

func TestPersistenceContract(t *testing.T) {
	contract := []struct{ name, got, want string }{
		{"settings hash key prefix", settingsKeyPrefix, "settings:"},
		{"commands section marker", commandsMarkerField, "commands:projected"},
		{"modules section marker", modulesMarkerField, "modules:projected"},
		{"fetches section marker", fetchesMarkerField, "fetches:projected"},
		{"command row field prefix", commandFieldPrefix, "command:"},
		{"alias pointer field prefix", aliasFieldPrefix, "cmdalias:"},
		{"fetch row field prefix", fetchFieldPrefix, "fetch:"},
		{"live counter hash prefix", liveCounterPrefix, "ctr:live:"},
		{"live board key prefix", liveBoardPrefix, "ctr:board:v2:"},
		{"live board member index prefix", liveBoardMemberPrefix, "ctr:board-member:v2:"},
		{"live board seed flag prefix", liveBoardSeedFlag, "ctr:board-seeded:v2:"},
		{"live batch receipt prefix", liveSeenPrefix, "ctr:seen:"},
	}
	for _, c := range contract {
		assert.Equal(t, c.want, c.got, c.name)
	}
}
