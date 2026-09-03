// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Module-section round trips against the in-process fake Valkey
// (fakevalkey_test.go), beside the fetch section's fetch_test.go.

// A module's logical row spans two hash fields, so an omitted config write is
// not an overwrite: clearing a config on the dashboard publishes an empty
// Configs, and skipping the write left the previous config readable forever.
func TestSetModuleClearsAnEmptiedConfig(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:81"

	require.NoError(t, store.SetModule(ctx, 81, ModuleView{
		Name:      "govee",
		IsEnabled: true,
		Configs:   codec.RawMessage(`{"device":"living-room"}`),
	}))
	require.Equal(t, `{"device":"living-room"}`, f.hash(key)["module:govee:config"])

	require.NoError(t, store.SetModule(ctx, 81, ModuleView{Name: "govee", IsEnabled: true}))

	// The contract is what a reader sees, not how the field is stored: the
	// cleared config is written as an empty field, which GetModules reads back
	// as a zero-length blob exactly like an absent one. Asserting deletion
	// would pin the storage detail and fail the atomic single-HSET form.
	assert.Empty(t, f.hash(key)["module:govee:config"], "the old config is gone from the hash")

	byName, _, err := store.GetModules(ctx, 81)
	require.NoError(t, err)
	require.Contains(t, byName, "govee")
	assert.Empty(t, byName["govee"].Configs, "GetModules must not keep serving the old config")
	assert.True(t, byName["govee"].IsEnabled, "the enabled flag still projects")
}

// The config delete must not reach any other module, nor the enabled flag it
// rides with.
func TestSetModuleConfigDeleteIsScopedToOneModule(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:82"

	require.NoError(t, store.SetModule(ctx, 82, ModuleView{Name: "timers", Configs: codec.RawMessage(`{"n":1}`)}))
	require.NoError(t, store.SetModule(ctx, 82, ModuleView{Name: "govee", IsEnabled: true, Configs: codec.RawMessage(`{"n":2}`)}))
	require.NoError(t, store.SetModule(ctx, 82, ModuleView{Name: "govee", IsEnabled: false}))

	h := f.hash(key)
	assert.Equal(t, `{"n":1}`, h["module:timers:config"], "a sibling module's config is untouched")
	assert.Equal(t, "0", h["module:govee:enabled"])
	assert.Empty(t, h["module:govee:config"], "the cleared module's config is gone")
}

// SetModule is a per-row write and must never declare the section complete.
func TestSetModuleNeverMarksTheSectionProjected(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetModule(ctx, 83, ModuleView{Name: "clip"}))

	_, projected, err := store.GetModules(ctx, 83)
	require.NoError(t, err)
	assert.False(t, projected)
}
