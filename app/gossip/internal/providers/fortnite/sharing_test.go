// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"ItsBagelBot/app/gossip/internal/core"
	"context"
	"net/http"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsAndSessionShareOneUpstreamFetch(t *testing.T) {
	var reqs []*http.Request
	p := newTestProvider(t, statsUpstream(t, "Ninja", syntheticBlob, &reqs), noUpstream(t, "shop"), nil)
	ctx := context.Background()

	stats := asStats(t, handle(t, p, "stats")(ctx, gossiprpc.Request{Account: "Ninja"}))
	require.Empty(t, stats.Error)
	warm := requestPaths(reqs)
	require.NotEmpty(t, warm, "the cold stats lookup must reach the upstream")

	sess := asSession(t, handle(t, p, "session")(ctx,
		gossiprpc.Request{Account: "Ninja", ChannelID: "42"}))
	require.Empty(t, sess.Error)
	assert.Equal(t, stats.Player, sess.Player)

	assert.Equal(t, warm, requestPaths(reqs),
		"a warm !fnstats must make the session path cost no upstream call at all")
}

func TestStatsEntryExpiryDoesNotRedoTheAccountResolve(t *testing.T) {
	var reqs []*http.Request
	p, store := newTestProviderWithStore(t, statsUpstream(t, "Ninja", syntheticBlob, &reqs), noUpstream(t, "shop"), nil)
	ctx := context.Background()

	require.Empty(t, asStats(t, handle(t, p, "stats")(ctx, gossiprpc.Request{Account: "Ninja"})).Error)
	require.Equal(t, []string{
		"/api/v1/account/displayName/Ninja",
		"/api/v2/stats/deadbeef",
	}, requestPaths(reqs), "a cold lookup pays both upstream calls, in series")

	require.NoError(t, store.Del(ctx, lifetimeStatsKey("Ninja")))

	require.Empty(t, asStats(t, handle(t, p, "stats")(ctx, gossiprpc.Request{Account: "Ninja"})).Error)
	assert.Equal(t, []string{
		"/api/v1/account/displayName/Ninja",
		"/api/v2/stats/deadbeef",
		"/api/v2/stats/deadbeef",
	}, requestPaths(reqs), "the refill must cost the stats call alone")
}

func TestSessionSurfacesSharedNegativeEntry(t *testing.T) {
	var reqs []*http.Request
	missing := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.Clone(context.Background()))
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Upstream API error: account not found"}`))
	})
	p := newTestProvider(t, missing, noUpstream(t, "shop"), nil)
	ctx := context.Background()

	sess := asSession(t, handle(t, p, "session")(ctx,
		gossiprpc.Request{Account: "Ghosty", ChannelID: "42"}))
	assert.Equal(t, "player not found", sess.Error)
	assert.False(t, sess.HasSnapshot)
	assert.Zero(t, sess.Matches, "a shaped failure must never read as a played session")

	before := len(reqs)
	sess = asSession(t, handle(t, p, "session")(ctx,
		gossiprpc.Request{Account: "Ghosty", ChannelID: "42"}))
	assert.Equal(t, "player not found", sess.Error)
	assert.Equal(t, before, len(reqs), "the negative entry must serve the repeat lookup")
}

func init() { core.SetSSRFCheckForTests(false) }
