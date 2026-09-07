// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSessionStartMissingChannel(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "henrik"), noUpstream(t, "content"))
	reply := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_start")(context.Background(),
		gossiprpc.Request{Account: "Frosty#EUW1"}))
	assert.Equal(t, "missing channel", reply.Error)
}

func TestSessionStartInvalidRiotIDStartsNoLoop(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "henrik"), noUpstream(t, "content"))
	reply := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_start")(context.Background(),
		gossiprpc.Request{Account: "not-a-riot-id", ChannelID: "chan-invalid"}))
	assert.NotEmpty(t, reply.Error)
	sessions.mu.Lock()
	_, ok := sessions.byID["chan-invalid"]
	sessions.mu.Unlock()
	assert.False(t, ok, "a rejected identity must not arm a warm loop")
}

// TestSessionStartIsIdempotentPerChannel pins session_start's dedup: a
// duplicate stream.online (or a sesame retry after a timed-out call) must
// join the channel's existing loop rather than spawn a second one, which
// would double the request spend against the 2 req/5min/broadcaster budget.
// session_end must then remove exactly that entry.
// silentUpstream answers every request with an empty body and never touches
// t: session_start warms immediately on a goroutine that may still be in
// flight when the test returns, and a t.Errorf from a finished test panics.
func silentUpstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "{}") })
}

func TestSessionStartIsIdempotentPerChannel(t *testing.T) {
	p := newTestProvider(t, silentUpstream(), noUpstream(t, "content"))
	channelID := "chan-idempotent"
	req := gossiprpc.Request{Account: "Frosty#EUW1", Region: "na", ChannelID: channelID}

	first := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_start")(context.Background(), req))
	require.Empty(t, first.Error)
	sessions.mu.Lock()
	_, ok := sessions.byID[channelID]
	sessions.mu.Unlock()
	require.True(t, ok, "session_start must arm the channel's loop")

	second := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_start")(context.Background(), req))
	assert.Empty(t, second.Error)
	sessions.mu.Lock()
	count := len(sessions.byID)
	sessions.mu.Unlock()
	assert.Equal(t, 1, count, "a duplicate session_start must not spawn a second loop")

	end := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_end")(context.Background(),
		gossiprpc.Request{ChannelID: channelID}))
	assert.Empty(t, end.Error)
	sessions.mu.Lock()
	_, stillThere := sessions.byID[channelID]
	sessions.mu.Unlock()
	assert.False(t, stillThere, "session_end must remove the channel's entry")
}

func TestSessionEndUnknownChannelIsNoop(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "henrik"), noUpstream(t, "content"))
	reply := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_end")(context.Background(),
		gossiprpc.Request{ChannelID: "chan-never-started"}))
	assert.Empty(t, reply.Error)
}

func TestSessionEndMissingChannel(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "henrik"), noUpstream(t, "content"))
	reply := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_end")(context.Background(), gossiprpc.Request{}))
	assert.Equal(t, "missing channel", reply.Error)
}

// TestWarmTickPopulatesTheCacheTheChatEndpointsRead is the load-bearing case:
// it proves a warm tick and a real !valrank/!valmatches call agree on the
// exact same cache entry (same key, same TTL, same budget admitter), so a
// chat command right after a tick is a hit, not a second upstream spend. It
// builds two *api instances sharing one core.Cache — the wired provider a
// chat command dispatches through, and a second reference standing in for
// the warm loop's own instance (gossip runs exactly one *api per process; two
// references are only needed here to call the unexported warmRank/warmMatches
// directly without waiting on the real 5-minute ticker).
func TestWarmTickPopulatesTheCacheTheChatEndpointsRead(t *testing.T) {
	rankHits, matchesHits := 0, 0
	anHourAgo := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	matchesBodyFilled := fmt.Sprintf(matchesBody, anHourAgo, anHourAgo, anHourAgo)
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valorant/v3/mmr/na/pc/Frosty/EUW1":
			rankHits++
			fmt.Fprint(w, mmrBody)
		case "/valorant/v4/matches/na/pc/Frosty/EUW1":
			matchesHits++
			fmt.Fprint(w, matchesBodyFilled)
		default:
			t.Errorf("unexpected henrik path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	henrikSrv := httptest.NewServer(henrik)
	t.Cleanup(henrikSrv.Close)
	cache := core.NewCache(newMemStore())
	cfg := Config{BaseURL: henrikSrv.URL, APIKey: "val-key"}
	d := provider.Deps{Cache: cache, Log: zap.NewNop()}

	wired := New(cfg, d)
	rankHandle := endpoint(t, wired, "rank")
	matchesHandle := endpoint(t, wired, "matches")

	b := provider.NewProvider(providerName, d).Trusted()
	warmer := newAPI(cfg, d, b)

	req := gossiprpc.Request{Account: "Frosty#EUW1", Region: "na"}
	warmer.warmRank(context.Background(), req)
	warmer.warmMatches(context.Background(), req)
	require.Equal(t, 1, rankHits, "one warm tick spends exactly one rank request")
	require.Equal(t, 1, matchesHits, "one warm tick spends exactly one matches request")

	rankReply := decodeReply[rankReply](t, rankHandle(context.Background(), req))
	assert.Empty(t, rankReply.Error)
	assert.Equal(t, "Immortal 1", rankReply.Tier)

	matchesReply := decodeReply[matchesReply](t, matchesHandle(context.Background(), req))
	assert.Empty(t, matchesReply.Error)
	assert.NotEmpty(t, matchesReply.Matches)

	assert.Equal(t, 1, rankHits, "!valrank after a warm tick must hit cache, not upstream")
	assert.Equal(t, 1, matchesHits, "!valmatches after a warm tick must hit cache, not upstream")

	// A second warm tick before the TTL elapses must also skip upstream: the
	// loop must not spend budget on an entry that is still fresh.
	warmer.warmRank(context.Background(), req)
	warmer.warmMatches(context.Background(), req)
	assert.Equal(t, 1, rankHits, "a warm tick within the TTL must not re-spend the rank budget")
	assert.Equal(t, 1, matchesHits, "a warm tick within the TTL must not re-spend the matches budget")
}

// A session_end that lands on a sibling replica only deletes the session key;
// the owning loop must notice and wind itself down without a local cancel.
func TestWarmLoopStopsWhenSessionKeyVanishes(t *testing.T) {
	prev := warmEvery
	warmEvery = 10 * time.Millisecond
	t.Cleanup(func() { warmEvery = prev })
	var hits atomic.Int32
	henrikSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "{}")
	}))
	t.Cleanup(henrikSrv.Close)
	cache := core.NewCache(newMemStore())
	p := New(Config{BaseURL: henrikSrv.URL, APIKey: "val-key"}, provider.Deps{Cache: cache, Log: zap.NewNop()})
	channelID := "chan-remote-end"
	req := gossiprpc.Request{Account: "Frosty#EUW1", Region: "na", ChannelID: channelID}
	start := decodeReply[gossiprpc.ValorantSessionReply](t, endpoint(t, p, "session_start")(context.Background(), req))
	require.Empty(t, start.Error)

	// Let the immediate warm land first so the delete is observed by a loop
	// that is genuinely ticking, not one that has not started yet.
	require.Eventually(t, func() bool { return hits.Load() > 0 },
		5*time.Second, 5*time.Millisecond, "first warm must reach the upstream")
	require.NoError(t, cache.DelJSON(context.Background(), sessionKey(channelID)))

	require.Eventually(t, func() bool {
		sessions.mu.Lock()
		defer sessions.mu.Unlock()
		_, still := sessions.byID[channelID]
		return !still
	}, 5*time.Second, 20*time.Millisecond, "loop must exit once the session key is gone")
}
