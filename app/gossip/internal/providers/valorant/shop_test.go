// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Shop tests are their own file for the same reason fortnite's shop_test.go
// is: the featured bundle is a three-upstream join (store payload, skin
// catalogue, rarity tiers) and its failure modes — unknown items, currency
// filtering, catalogue lag — share nothing with the per-player lookups the
// other tests cover.

package valorant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const testBundleAssetID = "d087f4fd-4942-d782-c76c-5e84dc307a66"

const (
	skinsBody = `{"status":200,"data":[
	  {"uuid":"skin-a","displayName":"Reaver Vandal","displayIcon":"https://media.test/reaver.png","contentTierUuid":"tier-ultra",
	   "levels":[{"uuid":"level-a1","displayName":"Reaver Vandal Level 1"},{"uuid":"level-a2","displayName":"Reaver Vandal Level 2"}]},
	  {"uuid":"skin-b","displayName":"Prime Phantom","displayIcon":"https://media.test/prime.png","contentTierUuid":"tier-deluxe",
	   "levels":[{"uuid":"level-b1","displayName":"Prime Phantom Level 1"}]}
	]}`
	tiersBody = `{"status":200,"data":[
	  {"uuid":"tier-ultra","displayName":"ULTRA Edition","backgroundColor":"#b8903ce6"},
	  {"uuid":"tier-deluxe","displayName":"Deluxe Edition","backgroundColor":"#fd7d02e6"}
	]}`
	bundleBody = `{"status":200,"data":{
	  "uuid":"` + testBundleAssetID + `",
	  "displayName":"Aeris",
	  "displayNameSubText":"Collection",
	  "description":"Make every angle yours.",
	  "displayIcon":"https://media.test/aeris.png"
	}}`
)

var featuredBody = fmt.Sprintf(`{"status":200,"data":{"FeaturedBundle":{"Bundle":{
  "ID":"69f5f8ea-3022-40f0-9d62-aefb7d09dcaf",
  "DataAssetID":%q,
  "CurrencyID":"85ad13f7-3d1b-5128-9eb2-7cd8ee0b5741",
  "TotalDiscountPercent":0.451,
  "DurationRemainingInSeconds":874496,
  "Items":[
    {"Item":{"ItemTypeID":"e7c63390-97b1-4a2a-8a2b-0b5cbbde9697","ItemID":"level-a1","Amount":1},
     "BasePrice":4350.0,"DiscountPercent":0.45,"DiscountedPrice":2393},
    {"Item":{"ItemTypeID":"e7c63390-97b1-4a2a-8a2b-0b5cbbde9697","ItemID":"level-b1","Amount":1},
     "BasePrice":2175.0,"DiscountPercent":0.45,"DiscountedPrice":1196},
    {"Item":{"ItemTypeID":"dd3bf334-87f3-405d-8fd7-ac8064ae2756","ItemID":"buddy-1","Amount":1},
     "BasePrice":325.0,"DiscountPercent":0.0,"DiscountedPrice":0},
    {"Item":{"ItemTypeID":"e7c63390-97b1-4a2a-8a2b-0b5cbbde9697","ItemID":"level-unknown","Amount":1},
     "BasePrice":900.0,"DiscountPercent":0.0,"DiscountedPrice":0}
  ]
}}}}`,
	testBundleAssetID)

func newShopProvider(t *testing.T) (provider.Provider, func() [4]int) {
	t.Helper()
	var mu sync.Mutex
	skinHits, tierHits, bundleHits, storeHits := 0, 0, 0, 0

	henrik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, "/valorant/v1/store-featured", r.URL.Path)
		storeHits++
		fmt.Fprint(w, featuredBody)
	}))
	content := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/weapons/skins":
			skinHits++
			fmt.Fprint(w, skinsBody)
		case "/v1/contenttiers":
			tierHits++
			fmt.Fprint(w, tiersBody)
		case "/v1/bundles/" + testBundleAssetID:
			bundleHits++
			fmt.Fprint(w, bundleBody)
		default:
			t.Errorf("unexpected content path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(func() { henrik.Close(); content.Close() })

	p := New(Config{
		BaseURL:        henrik.URL,
		ContentBaseURL: content.URL,
		APIKey:         "val-key",
	}, provider.Deps{
		Cache: core.NewCache(newMemStore()),
		Log:   zap.NewNop(),
	})
	hits := func() [4]int {
		mu.Lock()
		defer mu.Unlock()
		return [4]int{skinHits, tierHits, bundleHits, storeHits}
	}
	return p, hits
}

func TestShopJoinsTheFeaturedBundleAgainstTheCatalogue(t *testing.T) {
	p, hits := newShopProvider(t)
	handle := endpoint(t, p, "shop")

	reply := decodeReply[shopReply](t, handle(context.Background(), gossiprpc.Request{}))

	assert.Empty(t, reply.Error)
	assert.Equal(t, "Aeris", reply.Bundle)
	assert.Equal(t, "Collection", reply.Subtitle)
	assert.Equal(t, "https://media.test/aeris.png", reply.Icon)
	assert.InDelta(t, 45.1, reply.DiscountPct, 0.001)
	assert.Equal(t, int64(874496), reply.ExpiresSeconds, "Riot's own countdown rides through")
	assert.Equal(t, int64(2393+1196), reply.Price, "only the listed skins sum; sprays stay out")

	require.Len(t, reply.Items, 2, "non-skin items and catalogue misses never render")
	first := reply.Items[0]
	assert.Equal(t, "Reaver Vandal", first.Name, "bundle items key on level uuids, resolved to the parent skin")
	assert.Equal(t, int64(2393), first.Price, "discounted price wins over base")
	assert.Equal(t, "ULTRA Edition", first.Tier)
	assert.Equal(t, "#b8903ce6", first.Color)
	assert.Equal(t, int64(1196), reply.Items[1].Price)

	require.Equal(t, [4]int{1, 1, 1, 1}, hits(), "catalogues cache for a day; one store fetch serves the window")

	again := decodeReply[shopReply](t, handle(context.Background(), gossiprpc.Request{}))
	assert.Equal(t, reply, again)
	require.Equal(t, [4]int{1, 1, 1, 1}, hits(), "nothing re-fetches inside the six-hour window")
}

func TestShopUnknownSkinsAreSkippedButCountedHonestly(t *testing.T) {
	// A catalogue miss mid-patch shrinks Count rather than padding rows with
	// blank names.
	p, _ := newShopProvider(t)
	reply := decodeReply[shopReply](t, endpoint(t, p, "shop")(context.Background(), gossiprpc.Request{}))
	assert.Equal(t, 2, reply.Count)
	require.Len(t, reply.Items, reply.Count)
	for _, item := range reply.Items {
		assert.NotEmpty(t, strings.TrimSpace(item.Name))
		assert.Positive(t, item.Price)
	}
}
