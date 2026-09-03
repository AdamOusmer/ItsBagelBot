// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Round trips run against the in-process fake Valkey (fakevalkey_test.go):
// the delete path's HDEL, the section marker and the client's tiering are all
// decided from server state.

// newTestStore boots the fake Valkey and a Store over it.
func newTestStore(t *testing.T) (*Store, *fakeValkey) {
	t.Helper()
	f := newFakeValkey(t)
	return NewStore(f.client), f
}

func fetchDTO(userID uint64, name string, deleted bool) data.FetchChangedDTO {
	return data.FetchChangedDTO{
		UserID:   userID,
		Name:     name,
		URL:      "https://api.example.com/v1?city=berlin",
		JSONPath: []string{"current", "temp_c"},
		KeyLabel: "openweather",
		IsActive: true,
		Deleted:  deleted,
	}
}

func TestSetFetchGetFetchRoundTrip(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetFetch(ctx, fetchDTO(42, "Weather", false)))

	h := f.hash("settings:42")
	require.Contains(t, h, "fetch:weather", "field keyed by the normalized name")
	assert.JSONEq(t, `{"name":"weather","url":"https://api.example.com/v1?city=berlin","json_path":["current","temp_c"],"key_label":"openweather","is_active":true}`, h["fetch:weather"])

	view, found, projected, err := store.GetFetch(ctx, 42, "Weather")
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, projected, "per-row writes never declare the section complete")
	assert.Equal(t, "weather", view.Name)
	assert.Equal(t, []string{"current", "temp_c"}, view.JSONPath)
	assert.Equal(t, "openweather", view.KeyLabel)

	// Overwrite replaces the body in place.
	dto := fetchDTO(42, "weather", false)
	dto.KeyLabel = ""
	require.NoError(t, store.SetFetch(ctx, dto))
	view, found, _, err = store.GetFetch(ctx, 42, "weather")
	require.NoError(t, err)
	require.True(t, found)
	assert.Empty(t, view.KeyLabel)
}

func TestSetFetchDeletedRetiresField(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetFetch(ctx, fetchDTO(43, "wx", false)))
	require.NoError(t, store.SetFetch(ctx, fetchDTO(43, "wx", true)))

	_, found, projected, err := store.GetFetch(ctx, 43, "wx")
	require.NoError(t, err)
	assert.False(t, found, "delete must HDEL the fetch:<name> field")
	assert.False(t, projected)
}

func TestSetFetchesReplacesSectionAndMarksProjected(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:44"

	// Stale row from an earlier projection + a foreign field that survives.
	f.seed(key, fakeField{field: "fetch:old", value: `{"name":"old","url":"https://gone.example"}`})
	f.seed(key, fakeField{field: "status", value: "paid"})

	require.NoError(t, store.SetFetches(ctx, 44, []FetchView{
		{Name: "a", URL: "https://a.example.com"},
		{Name: "b", URL: "https://b.example.com", IsActive: true},
	}))

	h := f.hash(key)
	require.NotContains(t, h, "fetch:old", "section replace clears stale rows")
	assert.NotEmpty(t, h["fetch:a"])
	assert.NotEmpty(t, h["fetch:b"])
	assert.Equal(t, "1", h[fetchesMarkerField])
	assert.Equal(t, "paid", h["status"], "clear touches only prefixed fields")

	fetches, projected, err := store.GetFetches(ctx, 44)
	require.NoError(t, err)
	assert.True(t, projected)
	names := map[string]bool{}
	for _, f2 := range fetches {
		names[f2.Name] = true
	}
	assert.Equal(t, map[string]bool{"a": true, "b": true}, names)

	// An empty list is known data, not a cold miss.
	require.NoError(t, store.SetFetches(ctx, 44, nil))
	fetches, projected, err = store.GetFetches(ctx, 44)
	require.NoError(t, err)
	assert.True(t, projected)
	assert.Empty(t, fetches)
}

func TestGetFetchUnprojectedVsMissing(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()

	// Hash exists with other sections but no fetch marker: not projected.
	f.seed("settings:45", fakeField{field: "status", value: "free"})
	_, _, projected, err := store.GetFetch(ctx, 45, "anything")
	require.NoError(t, err)
	assert.False(t, projected)

	// Projected but absent name: a real negative.
	require.NoError(t, store.SetFetches(ctx, 45, []FetchView{{Name: "real", URL: "https://x.example"}}))
	_, found, projected, err := store.GetFetch(ctx, 45, "ghost")
	require.NoError(t, err)
	assert.True(t, projected)
	assert.False(t, found)

	view, found, _, err := store.GetFetch(ctx, 45, "real")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "https://x.example", view.URL)
}

// $(urlfetch) names are user-supplied, so a definition named "projected"
// must not be able to overwrite the section's completeness marker, and must
// itself stay readable as an ordinary row.
func TestFetchNamedProjectedCollidesWithNothing(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:47"

	require.NoError(t, store.SetFetches(ctx, 47, []FetchView{{Name: "real", URL: "https://x.example"}}))
	require.NoError(t, store.SetFetch(ctx, fetchDTO(47, "Projected", false)))

	assert.Equal(t, "1", f.hash(key)[fetchesMarkerField], "the marker survives a row of the same name")

	_, _, projected, err := store.GetFetch(ctx, 47, "real")
	require.NoError(t, err)
	assert.True(t, projected, "the section stays projected, so reads never fall through to the RPC")

	view, found, projected, err := store.GetFetch(ctx, 47, "projected")
	require.NoError(t, err)
	require.True(t, found, "a definition named projected is an ordinary row")
	assert.True(t, projected)
	assert.Equal(t, "https://api.example.com/v1?city=berlin", view.URL)

	fetches, projected, err := store.GetFetches(ctx, 47)
	require.NoError(t, err)
	assert.True(t, projected)
	names := map[string]bool{}
	for _, f2 := range fetches {
		names[f2.Name] = true
	}
	assert.Equal(t, map[string]bool{"real": true, "projected": true}, names, "the row is visible in the list too")
}

// Hashes written before the marker rename carry fetch:projected = "1". It is
// not a FetchView, so the list read skips it like any corrupt row instead of
// emitting a junk definition, and the section correctly reads as unprojected
// until a full-section write clears the prefix and lays down the new marker.
func TestLegacyFetchProjectedFieldDecaysHarmlessly(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:48"

	f.seed(key, fakeField{field: "fetch:projected", value: "1"})
	f.seed(key, fakeField{field: "fetch:wx", value: `{"name":"wx","url":"https://wx.example"}`})

	fetches, projected, err := store.GetFetches(ctx, 48)
	require.NoError(t, err)
	assert.False(t, projected, "the old marker no longer declares completeness")
	require.Len(t, fetches, 1)
	assert.Equal(t, "wx", fetches[0].Name)

	require.NoError(t, store.SetFetches(ctx, 48, []FetchView{{Name: "wx", URL: "https://wx.example"}}))
	h := f.hash(key)
	assert.NotContains(t, h, "fetch:projected", "the full-section write sweeps the legacy field")
	assert.Equal(t, "1", h[fetchesMarkerField])
}

func TestClientFetchDefsTiersAndNegativeCaching(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()

	client := NewClient(Config{
		Store:    store,
		Subjects: Subjects{Fetches: "bagel.rpc.internal.projection.commands.fetches.get"},
		TTL:      0, // entries never expire within the test
		Log:      zap.NewNop(),
	})
	defer client.Close()

	// Tier 3 would dial NATS; with the section unprojected the loader falls
	// through and (no NC wired here) caches the negative — proving the miss
	// path is safe, then proving a later full projection heals reads.
	_, found, err := client.FetchDefs(ctx, 46, "late")
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, store.SetFetches(ctx, 46, []FetchView{
		{Name: "late", URL: "https://late.example", KeyLabel: "k", IsActive: true},
	}))
	// The stale negative entry still governs until invalidated — push
	// invalidation's job (evictScope "fetches"), exercised directly here.
	client.fetches.Invalidate(fetchKey(46, "late"))

	view, found, err := client.FetchDefs(ctx, 46, "Late")
	require.NoError(t, err)
	require.True(t, found, "tier-2 hit after invalidation")
	assert.Equal(t, "https://late.example", view.URL)
	assert.Equal(t, "k", view.KeyLabel, "label projects; key material never does")

	opsBefore := len(f.ops())
	_, found, err = client.FetchDefs(ctx, 46, "late")
	require.NoError(t, err)
	assert.True(t, found)
	assert.GreaterOrEqual(t, len(f.ops()), opsBefore, "cache hit issues no server ops")
	assert.Equal(t, opsBefore, len(f.ops()), "warm entry costs zero Valkey round trips")

	// Unknown name on a projected user: cached negative.
	_, found, err = client.FetchDefs(ctx, 46, "ghost")
	require.NoError(t, err)
	assert.False(t, found)
	negOps := len(f.ops())
	_, found, err = client.FetchDefs(ctx, 46, "ghost")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, negOps, len(f.ops()), "negative entries keep repeat misses off Valkey")
}
