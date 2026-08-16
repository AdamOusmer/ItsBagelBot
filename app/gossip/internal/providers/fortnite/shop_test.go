// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package fortnite

// Tests for the item-shop endpoint. They live apart from fortnite_test.go
// because the shop shares nothing with the stats side beyond the provider that
// serves both: a different upstream, no account identity, no rate-limit lane of
// its own, and a cache boundary set by Epic's daily rotation rather than by a
// staleness budget.

import (
	"context"
	"net/http"
	"testing"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const shopBody = `{
	"status": 200,
	"data": {
		"date": "2026-07-09T00:00:00Z",
		"entries": [
			{"finalPrice": 2800, "bundle": {"name": "Peely Bundle"}, "brItems": [{"name": "Peely"}]},
			{"finalPrice": 1200, "brItems": [{"name": "Renegade Raider"}]},
			{"finalPrice": 500, "tracks": [{"title": "Never Gonna Give You Up"}]},
			{"finalPrice": 400}
		]
	}
}`

func TestShopNormalizesAndCaches(t *testing.T) {
	var hits int
	p := newTestProvider(t, noUpstream(t, "stats"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		require.Equal(t, "/v2/shop", r.URL.Path)
		// The shop upstream is public: the stats key must not leak into it.
		assert.Empty(t, r.Header.Get("x-api-key"))
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(shopBody))
	}), nil)
	h := handle(t, p, "shop")

	reply := asShop(t, h(context.Background(), gossiprpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "2026-07-09", reply.Date)
	// The nameless entry is dropped; the bundle keeps its bundle name.
	assert.Equal(t, 3, reply.Count)
	require.Len(t, reply.Entries, 3)
	assert.Equal(t, gossiprpc.FortniteShopEntry{Name: "Peely Bundle", Price: 2800}, reply.Entries[0])
	assert.Equal(t, gossiprpc.FortniteShopEntry{Name: "Renegade Raider", Price: 1200}, reply.Entries[1])
	assert.Equal(t, gossiprpc.FortniteShopEntry{Name: "Never Gonna Give You Up", Price: 500}, reply.Entries[2])

	// Second call is served from the cache.
	reply = asShop(t, h(context.Background(), gossiprpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, 1, hits)
}

// The shop's cache boundary is Epic's own swap, so the deadline must be the
// next 00:00 UTC read from an absolute instant — never from the pod's local
// wall clock, and never the current instant itself, which would leave a
// zero-length window at exactly the moment the new shop lands.
func TestNextShopRotationIsTheNextMidnightUTC(t *testing.T) {
	// A fixed zone rather than a tzdata lookup: the container the tests run in
	// is not guaranteed to carry a zone database.
	eastern := time.FixedZone("EDT", -4*60*60)
	tomorrow := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"a moment after a rotation", time.Date(2026, 8, 16, 0, 0, 1, 0, time.UTC), tomorrow},
		{"exactly on a rotation", time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), tomorrow},
		{"a second before the next", time.Date(2026, 8, 16, 23, 59, 59, 0, time.UTC), tomorrow},
		// 19:30 Eastern is 23:30 UTC the same day: prime stream time, half an
		// hour before the swap. Read in local time this would answer a day late.
		{"evening west of UTC", time.Date(2026, 8, 16, 19, 30, 0, 0, eastern), tomorrow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := nextShopRotation(tc.now)
			assert.True(t, got.Equal(tc.want), "want %s, got %s", tc.want, got)
			assert.True(t, got.After(tc.now), "the deadline must be in the future")
		})
	}
}
