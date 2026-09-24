// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"ItsBagelBot/app/gossip/internal/core"
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
		assert.Empty(t, r.Header.Get("x-api-key"))
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(shopBody))
	}), nil)
	h := handle(t, p, "shop")

	reply := asShop(t, h(context.Background(), gossiprpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "2026-07-09", reply.Date)
	assert.Equal(t, 3, reply.Count)
	require.Len(t, reply.Entries, 3)
	assert.Equal(t, gossiprpc.FortniteShopEntry{Name: "Peely Bundle", Price: 2800}, reply.Entries[0])
	assert.Equal(t, gossiprpc.FortniteShopEntry{Name: "Renegade Raider", Price: 1200}, reply.Entries[1])
	assert.Equal(t, gossiprpc.FortniteShopEntry{Name: "Never Gonna Give You Up", Price: 500}, reply.Entries[2])

	reply = asShop(t, h(context.Background(), gossiprpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, 1, hits)
}

func TestNextShopRotationIsTheNextMidnightUTC(t *testing.T) {
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
		{"evening west of UTC", time.Date(2026, 8, 16, 19, 30, 0, 0, eastern), tomorrow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := nextShopRotation(tc.now)
			assert.True(t, got.Equal(tc.want), "want %s, got %s", tc.want, got)
			assert.True(t, got.After(tc.now), "the deadline must be in the future")
		})
	}
}

func init() { core.SetSSRFCheckForTests(false) }
