// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"

	"ItsBagelBot/internal/valkeytest"
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

func TestModuleRevisionSurvivesProjection(t *testing.T) {
	store, _ := newTestStore(t)
	require.NoError(t, store.SetModules(context.Background(), 84, []ModuleView{{Name: "automod", IsEnabled: true, Revision: 7}}))
	got, projected, err := store.GetModules(context.Background(), 84)
	require.NoError(t, err)
	assert.True(t, projected)
	assert.Equal(t, 7, got["automod"].Revision)
}

func TestSetModuleRejectsDelayedRevision(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.SetModule(ctx, 85, ModuleView{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"block_terms":"new"}`), Revision: 2}))
	require.NoError(t, store.SetModule(ctx, 85, ModuleView{Name: "automod", IsEnabled: false, Configs: codec.RawMessage(`{"block_terms":"old"}`), Revision: 1}))
	got, _, err := store.GetModules(ctx, 85)
	require.NoError(t, err)
	assert.True(t, got["automod"].IsEnabled)
	assert.Equal(t, 2, got["automod"].Revision)
	assert.JSONEq(t, `{"block_terms":"new"}`, string(got["automod"].Configs))
}

// A hydration reply can race an event projection. Its older row must not undo
// the event, and a module created after the hydration query must survive even
// when that snapshot does not contain it.
func TestSetModulesPreservesNewerConcurrentModuleRows(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.SetModule(ctx, 86, ModuleView{Name: "automod", IsEnabled: true, Configs: codec.RawMessage(`{"block_terms":"new"}`), Revision: 2}))
	require.NoError(t, store.SetModule(ctx, 86, ModuleView{Name: "timers", IsEnabled: true, Configs: codec.RawMessage(`{"seconds":30}`), Revision: 3}))
	require.NoError(t, store.SetModules(ctx, 86, []ModuleView{{Name: "automod", IsEnabled: false, Configs: codec.RawMessage(`{"block_terms":"old"}`), Revision: 1}}))
	got, projected, err := store.GetModules(ctx, 86)
	require.NoError(t, err)
	assert.True(t, projected)
	assert.True(t, got["automod"].IsEnabled)
	assert.Equal(t, 2, got["automod"].Revision)
	assert.JSONEq(t, `{"block_terms":"new"}`, string(got["automod"].Configs))
	assert.Equal(t, 3, got["timers"].Revision)
	assert.JSONEq(t, `{"seconds":30}`, string(got["timers"].Configs))
}

func seedCustomModule(f *fakeValkey) {
	for name, value := range map[string]string{
		modulesMarkerField:                      "1",
		"module:custom-name:enabled":            "1",
		"module:custom-name:config":             `{"text":"hello"}`,
		"module:custom-name:revision":           "7",
		"module:custom-name:account_created_at": "123",
		"command:large":                         "x",
		"fetch:large":                           "y",
		"locale":                                "fr",
	} {
		f.seed("settings:81", fakeField{name, value})
	}
}

func countOps(f *fakeValkey, cmd string) int {
	n := 0
	for _, op := range f.Ops() {
		if op.Cmd == cmd {
			n++
		}
	}
	return n
}

func TestGetModulesSnapshotRead(t *testing.T) {
	cases := []struct {
		name          string
		evalFail      string
		wantErr       bool
		wantFullReads int
		wantRevision  int
	}{
		{name: "reads the snapshot script without a full hash read", wantRevision: 7},
		{name: "falls back to one full hash read when EVAL_RO is unknown", evalFail: "unknown command 'EVAL_RO'", wantFullReads: 1, wantRevision: 7},
		{name: "falls back to one full hash read when EVAL_RO is not permitted", evalFail: "NOPERM cannot execute EVAL_RO", wantFullReads: 1, wantRevision: 7},
		{name: "ordinary failures gain no sequential retry", evalFail: "simulated transport failure", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, f := newTestStore(t)
			seedCustomModule(f)
			if tc.evalFail != "" {
				f.Fail(valkeytest.Failure{Cmd: "EVAL_RO", Message: tc.evalFail})
			}

			mods, projected, err := store.GetModules(context.Background(), 81)

			assert.Equal(t, tc.wantFullReads, countOps(f, "HGETALL"))
			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, !tc.wantErr, projected)
			assert.Equal(t, tc.wantRevision, mods["custom-name"].Revision)
		})
	}
}

func TestGetModulesProjectsEveryModuleField(t *testing.T) {
	store, f := newTestStore(t)
	seedCustomModule(f)

	mods, projected, err := store.GetModules(context.Background(), 81)

	require.NoError(t, err)
	assert.True(t, projected)
	assert.EqualValues(t, 123, mods["custom-name"].AccountCreatedAt)
	assert.True(t, mods["custom-name"].IsEnabled)
	assert.JSONEq(t, `{"text":"hello"}`, string(mods["custom-name"].Configs))
}
