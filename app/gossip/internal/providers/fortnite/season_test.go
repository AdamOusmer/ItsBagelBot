package fortnite

// Tests for the season window's resolution and its bound. They live apart from
// fortnite_test.go because they turn on the concurrency of resolveStatsWindow
// rather than on the stats aggregation the main file covers.

import (
	"context"
	"net/http"
	"testing"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
)

// slowSeasonUpstream is the fake api-fortnite.com behind
// TestStatsSeasonLegIsBounded. Its season endpoint never answers: it parks
// until its own request context dies, which happens only when the client hangs
// up. That leaves the client-side deadline as the one thing that can end the
// season leg, so the handler's wall time reports which deadline actually won —
// resolveStatsWindow's seasonResolveTimeout, or the 10s httpTimeout that was
// the only bound left once context.WithoutCancel dropped the deadline. The
// "Ghosty" lookup 404s immediately, so the caller-visible answer is settled in
// milliseconds and every millisecond past that is g.Wait() held open by the
// season leg alone.
type slowSeasonUpstream struct {
	// seasonEnded carries the season request's context error once the client
	// hangs up. A channel rather than a field: the handler goroutine outlives
	// the client call it is being observed from.
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

// parkUntilHangup blocks the season response until the client gives up. The
// safety net past httpTimeout exists only so a regression fails the timing
// assertions instead of wedging the suite until its own deadline.
func (u *slowSeasonUpstream) parkUntilHangup(t *testing.T, r *http.Request) {
	t.Helper()
	select {
	case <-r.Context().Done():
		u.seasonEnded <- r.Context().Err()
	case <-time.After(httpTimeout + 5*time.Second):
		t.Error("the season request was never cut off by any deadline")
	}
}

// TestStatsSeasonLegIsBounded pins the half of resolveStatsWindow's contract
// that context.WithoutCancel silently gave away: it insulates the season leg
// from the account leg's cancellation, but it also returns a context with no
// deadline, so the leg was left bounded by nothing nearer than httpTimeout.
// Since g.Wait() waits for both legs, that turned a fast account-leg failure
// (a mistyped display name, 404 in milliseconds) into a caller-visible stall
// for as long as a cold season fetch against a stuck upstream took. The
// season leg must now be capped by seasonResolveTimeout, and the account
// error must still be the one that comes back.
func TestStatsSeasonLegIsBounded(t *testing.T) {
	up := newSlowSeasonUpstream()
	p := newTestProvider(t, up.handler(t), noUpstream(t, "shop"), nil)

	start := time.Now()
	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "Ghosty", TimeWindow: "season"}))
	elapsed := time.Since(start)

	// The account leg still owns the answer; the season leg stays best-effort.
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

// The p.seasonStart > 0 override still yields the configured value with no
// concurrent machinery: since resolveStatsWindow takes the plain serial path,
// a broken season endpoint (which would 500 the concurrent path's fetch, were
// it reachable) never gets called and never affects the result.
