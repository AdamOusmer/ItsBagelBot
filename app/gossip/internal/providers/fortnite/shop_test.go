// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/providertest"
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
	p := newTestProvider(t, providertest.Forbid(t), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		require.Equal(t, "/v2/shop", r.URL.Path)
		assert.Empty(t, r.Header.Get("x-api-key"))
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(shopBody))
	}))

	reply := providertest.Call[gossiprpc.FortniteShopReply](t, p, "shop", gossiprpc.Request{})
	again := providertest.Call[gossiprpc.FortniteShopReply](t, p, "shop", gossiprpc.Request{})

	assert.Equal(t, gossiprpc.FortniteShopReply{
		Date:  "2026-07-09",
		Count: 3,
		Entries: []gossiprpc.FortniteShopEntry{
			{Name: "Peely Bundle", Price: 2800},
			{Name: "Renegade Raider", Price: 1200},
			{Name: "Never Gonna Give You Up", Price: 500},
		},
	}, reply)
	assert.Equal(t, reply, again)
	assert.Equal(t, 1, hits)
}

func TestShopAnswersAnOutageInChat(t *testing.T) {
	p := newTestProvider(t, providertest.Forbid(t), providertest.Respond(http.StatusBadGateway, `{}`))

	reply := providertest.Call[gossiprpc.FortniteShopReply](t, p, "shop", gossiprpc.Request{})

	assert.Equal(t, gossiprpc.FortniteShopReply{Error: "item shop lookup failed"}, reply)
}

func TestShopIsServedWithoutAnAPIKey(t *testing.T) {
	p := newTestProvider(t, providertest.Forbid(t), providertest.Respond(http.StatusOK, shopBody),
		func(cfg *Config) { cfg.APIKey = "" })

	reply := providertest.Call[gossiprpc.FortniteShopReply](t, p, "shop", gossiprpc.Request{})

	require.Empty(t, reply.Error)
	assert.Equal(t, 3, reply.Count)
}

func TestShopEntryExpiresAtTheNextRotation(t *testing.T) {
	p, store := newProviderWithStore(t, providertest.Forbid(t), providertest.Respond(http.StatusOK, shopBody))

	require.Empty(t, providertest.Call[gossiprpc.FortniteShopReply](t, p, "shop", gossiprpc.Request{}).Error)

	now := time.Now().UTC()
	rotation := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	retention := store.Retention("gossip:fortnite:shop:current")
	assert.WithinDuration(t, rotation, now.Add(retention), 5*time.Second,
		"the item shop rotates at midnight UTC, so the entry must fall out of the store then, not after")
}
