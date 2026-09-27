// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleSnapshotExcludesUnrelatedBodiesAndRetainsFencing(t *testing.T) {
	store, f := newTestStore(t)
	for name, value := range map[string]string{
		modulesMarkerField:                      "1",
		"module:custom-name:enabled":            "1",
		"module:custom-name:config":             `{"text":"hello"}`,
		"module:custom-name:revision":           "7",
		"module:custom-name:account_created_at": "123",
		"command:large":                         strings.Repeat("x", 100000),
		"fetch:large":                           strings.Repeat("y", 100000),
		"locale":                                "fr",
	} {
		f.seed("settings:81", fakeField{name, value})
	}
	ctx := context.Background()
	fields, err := store.client.Do(ctx, store.client.B().EvalRo().Script(moduleSnapshotRead).Numkeys(1).Key("settings:81").Build()).AsStrMap()
	require.NoError(t, err)
	require.Len(t, fields, 5, "only marker and module fields cross the wire")
	mods, projected, err := store.GetModules(ctx, 81)
	require.NoError(t, err)
	require.True(t, projected)
	require.Equal(t, 7, mods["custom-name"].Revision)
	require.EqualValues(t, 123, mods["custom-name"].AccountCreatedAt)
	require.JSONEq(t, `{"text":"hello"}`, string(mods["custom-name"].Configs))
}

func TestModuleSnapshotCompatibilityFallback(t *testing.T) {
	for _, reply := range []string{"unknown command 'EVAL_RO'", "NOPERM cannot execute EVAL_RO", "simulated transport failure"} {
		t.Run(reply, func(t *testing.T) {
			store, f := newTestStore(t)
			f.seed("settings:81", fakeField{modulesMarkerField, "1"})
			f.seed("settings:81", fakeField{"module:loyalty:revision", "5"})
			f.evalROFail = reply
			mods, projected, err := store.GetModules(context.Background(), 81)
			fallback := !strings.Contains(reply, "transport")
			if fallback {
				require.NoError(t, err)
				require.True(t, projected)
				require.Equal(t, 5, mods["loyalty"].Revision)
			} else {
				require.Error(t, err)
			}
			var fullReads int
			for _, op := range f.ops() {
				if op.cmd == "HGETALL" {
					fullReads++
				}
			}
			if fallback {
				require.Equal(t, 1, fullReads)
			} else {
				require.Zero(t, fullReads, "ordinary failures must not gain sequential retries")
			}
		})
	}
}
