// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"strings"
	"testing"

	"ItsBagelBot/pkg/cache"
	"github.com/stretchr/testify/require"
)

func TestModuleSnapshotLiveFiltersWithoutChangingHash(t *testing.T) {
	f := newLiveFixture(t)
	key := cache.UserKey(settingsKeyPrefix, f.user)
	t.Cleanup(func() { f.client.Do(f.ctx, f.client.B().Del().Key(key).Build()) })
	expected := map[string]string{
		modulesMarkerField:                 "1",
		"module:custom:enabled":            "1",
		"module:custom:revision":           "7",
		"module:custom:account_created_at": "123",
		"module:custom:config":             `{"text":"hello"}`,
	}
	fields := f.client.B().Hset().Key(key).FieldValue()
	for field, value := range expected {
		fields = fields.FieldValue(field, value)
	}
	fields = fields.FieldValue("command:large", strings.Repeat("x", 100000)).FieldValue("fetch:large", strings.Repeat("y", 100000)).FieldValue("locale", "fr")
	require.NoError(t, f.client.Do(f.ctx, fields.Build()).Error())
	before, err := f.client.Do(f.ctx, f.client.B().Hgetall().Key(key).Build()).AsStrMap()
	require.NoError(t, err)
	filtered, err := readModuleSnapshot(f.ctx, f.client, key)
	require.NoError(t, err)
	require.Equal(t, expected, filtered)
	mods, projected, err := f.store.GetModulesPrimary(f.ctx, f.user)
	require.NoError(t, err)
	require.True(t, projected)
	require.Equal(t, 7, mods["custom"].Revision)
	require.EqualValues(t, 123, mods["custom"].AccountCreatedAt)
	after, err := f.client.Do(f.ctx, f.client.B().Hgetall().Key(key).Build()).AsStrMap()
	require.NoError(t, err)
	require.Equal(t, before, after, "read-only snapshot must preserve the settings hash")
}
