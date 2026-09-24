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
)

type slowSeasonUpstream struct {
	seasonEnded chan error
}

func newSlowSeasonUpstream() *slowSeasonUpstream {
	return &slowSeasonUpstream{seasonEnded: make(chan error, 1)}
}

func (u *slowSeasonUpstream) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/season":
			u.parkUntilHangup(t, r)
		case "/api/v1/account/displayName/Ghosty":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":404,"error":"Upstream API error: not found"}`))
		default:
			t.Errorf("unexpected stats-upstream path %s", r.URL.Path)
		}
	})
}

func (u *slowSeasonUpstream) parkUntilHangup(t *testing.T, r *http.Request) {
	t.Helper()
	select {
	case <-r.Context().Done():
		u.seasonEnded <- r.Context().Err()
	case <-time.After(httpTimeout + 5*time.Second):
		t.Error("the season request was never cut off by any deadline")
	}
}

func TestStatsSeasonLegIsBounded(t *testing.T) {
	up := newSlowSeasonUpstream()
	p := newTestProvider(t, up.handler(t), noUpstream(t, "shop"), nil)

	start := time.Now()
	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "Ghosty", TimeWindow: "season"}))
	elapsed := time.Since(start)

	assert.Equal(t, "player not found", reply.Error)

	assert.GreaterOrEqual(t, elapsed, seasonResolveTimeout,
		"the season leg cannot end before its own bound")
	assert.Less(t, elapsed, seasonResolveTimeout+2*time.Second,
		"the season leg must be released by seasonResolveTimeout, not by httpTimeout")

	select {
	case err := <-up.seasonEnded:
		assert.Error(t, err, "the season request must be cut off by its own deadline")
	case <-time.After(2 * time.Second):
		t.Fatal("the season upstream never saw the client hang up")
	}
}

func init() { core.SetSSRFCheckForTests(false) }
