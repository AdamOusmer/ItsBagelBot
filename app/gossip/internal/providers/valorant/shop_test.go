// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Shop tests are their own file for the same reason fortnite's shop_test.go
// is: the offer rotation is a deadline-cached join of two upstreams, and its
// failure modes (boundary math, unknown items, currency filtering) share
// nothing with the per-player lookups the other tests cover.

package valorant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNextShopRotation(t *testing.T) {
	// The shop flips at 20:00 Pacific, pinned here as 03:00 UTC (the PDT
	// mapping). The table pins both sides of the boundary plus the instant
	// itself: at exactly 03:00 the new rotation is already live, so the
	// deadline is tomorrow, not "now" — a zero-length window would make the
	// flow answer uncached all day.
	day := func(day int, h, m int) time.Time {
		return time.Date(2026, time.August, day, h, m, 0, 0, time.UTC)
	}
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{now: day(20, 2, 59), want: day(20, 3, 0)}, // minutes before the flip
		{now: day(20, 3, 0), want: day(21, 3, 0)},  // the flip instant: next window
		{now: day(20, 12, 0), want: day(21, 3, 0)}, // midday
	}
	for _, tc := range cases {
		got := nextShopRotation(tc.now)
		assert.Equal(t, tc.want, got, "now=%s", tc.now)
		assert.Equal(t, time.UTC, got.Location())
	}
}

func newShopProvider(t *testing.T) (provider.Provider, func() [3]int) {
	t.Helper()
	var mu sync.Mutex
	skinHits, tierHits, offerHits := 0, 0, 0

	henrik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, "/valorant/v1/store-offers", r.URL.Path)
		offerHits++
		fmt.Fprint(w, offersBody)
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
	hits := func() [3]int {
		mu.Lock()
		defer mu.Unlock()
		return [3]int{skinHits, tierHits, offerHits}
	}
	return p, hits
}

const (
	skinsBody = `{"status":200,"data":[
	  {"uuid":"skin-a","displayName":"Reaver Vandal","displayIcon":"https://media.test/reaver.png","contentTierUuid":"tier-ultra"},
	  {"uuid":"skin-b","displayName":"Prime Phantom","displayIcon":"https://media.test/prime.png","contentTierUuid":"tier-deluxe"},
	  {"uuid":"skin-b-lvl1","displayName":"Standard Phantom","displayIcon":"","contentTierUuid":""}
	]}`
	tiersBody = `{"status":200,"data":[
	  {"uuid":"tier-ultra","displayName":"ULTRA Edition","backgroundColor":"#b8903ce6"},
	  {"uuid":"tier-deluxe","displayName":"Deluxe Edition","backgroundColor":"#fd7d02e6"}
	]}`
)

var offersBody = fmt.Sprintf(`{"status":200,"data":{"Offers":[
  {"OfferID":"skin-a","IsDirectPurchase":true,
   "Cost":{%q:3550},
   "Rewards":[{"ItemTypeID":%q,"ItemID":"lvl-a","Quantity":1}]},
  {"OfferID":"skin-b","IsDirectPurchase":true,
   "Cost":{%q:1775},
   "Rewards":[{"ItemTypeID":%q,"ItemID":"lvl-b","Quantity":1}]},
  {"OfferID":"rad-offer","IsDirectPurchase":true,
   "Cost":{"8579f7d4-2fbe-4a3d-a814-10e6e9e53c48":100},
   "Rewards":[{"ItemTypeID":"d06b97d2-4db6-469a-9067-89a957badc47","ItemID":"rad","Quantity":50}]},
  {"OfferID":"skin-unknown","IsDirectPurchase":true,
   "Cost":{%q:900},
   "Rewards":[{"ItemTypeID":%q,"ItemID":"lvl-u","Quantity":1}]},
  {"OfferID":"no-vp","IsDirectPurchase":true,
   "Cost":{"8579f7d4-2fbe-4a3d-a814-10e6e9e53c48":400},
   "Rewards":[{"ItemTypeID":%q,"ItemID":"lvl-nv","Quantity":1}]}
]}}`,
	valorantPointsTypeID, skinItemTypeID,
	valorantPointsTypeID, skinItemTypeID,
	valorantPointsTypeID, skinItemTypeID,
	skinItemTypeID)

func TestShopJoinsPricesAgainstTheCatalogue(t *testing.T) {
	p, hits := newShopProvider(t)
	handle := endpoint(t, p, "shop")

	reply := decodeReply[shopReply](t, handle(context.Background(), gossiprpc.Request{}))

	assert.Empty(t, reply.Error)
	require.Len(t, reply.Items, 2, "radianite offers, VP-less offers and catalogue misses never render")

	first := reply.Items[0]
	assert.Equal(t, "Reaver Vandal", first.Name, "priciest first")
	assert.Equal(t, int64(3550), first.Price)
	assert.Equal(t, "ULTRA Edition", first.Tier)
	assert.Equal(t, "#b8903ce6", first.Color)
	assert.Equal(t, "https://media.test/reaver.png", first.Icon)
	assert.Equal(t, int64(1775), reply.Items[1].Price)

	resetIn := time.Unix(reply.ResetUnix, 0).Sub(time.Now())
	assert.Greater(t, resetIn, time.Duration(0))
	assert.LessOrEqual(t, resetIn, 24*time.Hour, "reset lands inside the coming rotation day")
	assert.False(t, reply.Empty)

	require.Equal(t, [3]int{1, 1, 1}, hits(), "catalogues cache for a day; one offer fetch serves the window")

	again := decodeReply[shopReply](t, handle(context.Background(), gossiprpc.Request{}))
	assert.Equal(t, reply, again)
	require.Equal(t, [3]int{1, 1, 1}, hits(), "nothing re-fetches inside the same rotation window")
}

func TestUnknownSkinsAreSkippedButCountedHonestly(t *testing.T) {
	// A catalogue miss mid-patch shrinks Count rather than padding rows with
	// blank names; Empty stays false because real items did render.
	p, _ := newShopProvider(t)
	reply := decodeReply[shopReply](t, endpoint(t, p, "shop")(context.Background(), gossiprpc.Request{}))
	assert.Equal(t, 2, reply.Count)
	require.Len(t, reply.Items, reply.Count)
	for _, item := range reply.Items {
		assert.NotEmpty(t, strings.TrimSpace(item.Name))
		assert.Positive(t, item.Price)
	}
}
