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

	assert.Empty(t, f.hash(key)["module:govee:config"], "the old config is gone from the hash")

	byName, _, err := store.GetModules(ctx, 81)
	require.NoError(t, err)
	require.Contains(t, byName, "govee")
	assert.Empty(t, byName["govee"].Configs, "GetModules must not keep serving the old config")
	assert.True(t, byName["govee"].IsEnabled, "the enabled flag still projects")
}

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

func TestSetModuleNeverMarksTheSectionProjected(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetModule(ctx, 83, ModuleView{Name: "clip"}))

	_, projected, err := store.GetModules(ctx, 83)
	require.NoError(t, err)
	assert.False(t, projected)
}
