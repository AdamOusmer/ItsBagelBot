package fortnite

// Tests for the cache entry !fnstats and !fn share. They live apart from
// fortnite_test.go because they exercise the relationship BETWEEN the two
// endpoints rather than either endpoint on its own.

import (
	"context"
	"net/http"
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// !fnstats and !fn ask the upstream the same question — the lifetime counter
// blob for one account — so they must answer out of ONE cache entry. They used
// to keep separate keys (stats:lifetime:<acct> and session-live:<acct>), which
// cost a channel running both commands two account resolves and two
// /api/v2/stats calls for identical numbers.
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

// The shared entry holds friendly failures as ordinary replies, so a session
// read can decode to a reply carrying Error and zero counters. Diffing that
// against a snapshot would report a clean session for a player the upstream has
// never heard of; the error has to surface instead.
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
