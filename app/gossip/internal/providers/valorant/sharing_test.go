// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// This file tests the relationship *between* endpoints, not any one of them:
// which lookups share the day-long identity resolve and which deliberately do
// not. That contract is invisible in any single endpoint's test, and it is
// exactly what a future refactor of resolveAccount could silently break.

package valorant

import (
	"ItsBagelBot/app/gossip/internal/core"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
)

func TestAutoRegionResolveIsSharedAcrossEndpoints(t *testing.T) {
	var mu sync.Mutex
	accountHits := 0
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/valorant/v2/account/"):
			mu.Lock()
			accountHits++
			mu.Unlock()
			fmt.Fprint(w, accountBody)
		case strings.HasPrefix(r.URL.Path, "/valorant/v4/matches/"):
			fmt.Fprint(w, `{"status":200,"data":[]}`)
		default:
			fmt.Fprint(w, mmrBody)
		}
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))
	rank := endpoint(t, p, "rank")
	matches := endpoint(t, p, "matches")
	ctx := context.Background()
	auto := gossiprpc.Request{Account: "Frosty#EUW1"}

	assert.Empty(t, decodeReply[rankReply](t, rank(ctx, auto)).Error)
	assert.Empty(t, decodeReply[matchesReply](t, matches(ctx, auto)).Error)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, accountHits,
		"both auto-region lookups ride one identity entry; a second account read here means the shared resolve was lost")

	secondRank := decodeReply[rankReply](t, rank(ctx, gossiprpc.Request{Account: "Frosty#EUW1", Region: "eu"}))
	assert.Empty(t, secondRank.Error)
	assert.Equal(t, 1, accountHits,
		"an explicit region never touches the resolve at all")
}

func TestAccountEndpointKeepsItsOwnCacheEntry(t *testing.T) {
	// The account endpoint answers with level/card/title, which drift within
	// the identity resolve's 24h window; it intentionally reads upstream under
	// its own hour-long byte-flow entry instead of the resolve's. The sequence
	// below pins the deliberate duplication: warming the resolve first does
	// not spare the account endpoint one upstream read, and the account
	// endpoint's own second call is served from its flow cache.
	var mu sync.Mutex
	accountHits := 0
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/valorant/v2/account/") {
			mu.Lock()
			accountHits++
			mu.Unlock()
			fmt.Fprint(w, accountBody)
			return
		}
		fmt.Fprint(w, mmrBody)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))
	ctx := context.Background()

	assert.Empty(t, decodeReply[rankReply](t, endpoint(t, p, "rank")(ctx,
		gossiprpc.Request{Account: "Frosty#EUW1"})).Error, "warms the identity resolve")

	req := gossiprpc.Request{Account: "Frosty#EUW1"}
	assert.Empty(t, decodeReply[accountReply](t, endpoint(t, p, "account")(ctx, req)).Error,
		"must not ride the still-warm resolve entry")

	mu.Lock()
	hitsAfterFirst := accountHits
	mu.Unlock()
	assert.Equal(t, 2, hitsAfterFirst)

	assert.Empty(t, decodeReply[accountReply](t, endpoint(t, p, "account")(ctx, req)).Error)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, accountHits, "the account endpoint's own byte-flow cache absorbs the repeat")
}

// These tests stage plain-http loopback upstreams the gate rightly refuses;
// production binaries never set this (see core.SetSSRFCheckForTests).
func init() { core.SetSSRFCheckForTests(false) }
