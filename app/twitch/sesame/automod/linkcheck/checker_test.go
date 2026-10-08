// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dohClean    = `{"Status":0,"Answer":[{"type":1,"data":"1.2.3.4"}]}`
	dohSinkhole = `{"Status":0,"Answer":[{"type":1,"data":"0.0.0.0"}]}`
)

type fakeDoH struct {
	mu    sync.Mutex
	names []string
	reply func(call int, host string) (int, string)
}

func (f *fakeDoH) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("name")
	f.mu.Lock()
	f.names = append(f.names, host)
	call := len(f.names)
	f.mu.Unlock()
	status, body := f.reply(call, host)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (f *fakeDoH) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for _, name := range f.names {
		if !strings.HasPrefix(name, "barrier") {
			names = append(names, name)
		}
	}
	return names
}

func (f *fakeDoH) answered(host string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.names, host)
}

func sinkholeDohbad(_ int, host string) (int, string) {
	if strings.HasPrefix(host, "dohbad") {
		return http.StatusOK, dohSinkhole
	}
	return http.StatusOK, dohClean
}

var clockSkew atomic.Int64

func init() {
	nowNanos = func() int64 { return time.Now().UnixNano() + clockSkew.Load() }
}

func skewClock(t *testing.T) func(advance time.Duration) {
	t.Helper()
	clockSkew.Store(0)
	t.Cleanup(func() { clockSkew.Store(0) })
	return func(d time.Duration) { clockSkew.Add(int64(d)) }
}

type harness struct {
	checker  *Checker
	doh      *fakeDoH
	hits     chan Hit
	barriers atomic.Int64
}

func newHarness(t *testing.T, expander *Expander, reply func(int, string) (int, string)) *harness {
	t.Helper()

	feedsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("feedbad.example/scam\n"))
	}))
	t.Cleanup(feedsSrv.Close)

	h := &harness{doh: &fakeDoH{reply: reply}, hits: make(chan Hit, 16)}
	dohSrv := httptest.NewServer(h.doh)
	t.Cleanup(dohSrv.Close)

	feeds := NewFeeds([]FeedSource{{Name: "test", URL: feedsSrv.URL, Format: FormatLines}}, nil)
	_, err := feeds.Refresh(context.Background())
	require.NoError(t, err)

	h.checker = NewChecker(Options{
		ExpandShorteners: true,
		Workers:          1,
		Feeds:            feeds,
		DoH:              NewDoH(dohSrv.URL, nil),
		Expander:         expander,
	})
	h.checker.OnBad = func(hit Hit) { h.hits <- hit }

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.checker.Start(ctx)
	return h
}

func (h *harness) settle(t *testing.T) {
	t.Helper()
	host := fmt.Sprintf("barrier%d.example", h.barriers.Add(1))
	h.checker.Evaluate(host, 0, "")
	h.eventually(t, func() bool { return h.doh.answered(host) })
}

func (h *harness) queries(host string) int {
	n := 0
	for _, name := range h.doh.asked() {
		if name == host {
			n++
		}
	}
	return n
}

func (h *harness) eventually(t *testing.T, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 2*time.Second, 2*time.Millisecond)
}

func TestEvaluateConvictsFeedListedHostsInEveryLinkShape(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"a url with a scheme", "join https://feedbad.example/x now", true},
		{"a scheme-less subdomain", "check sub.feedbad.example/x for the event", true},
		{"a deep subdomain through the parent walk", "see deep.a.b.feedbad.example today", true},
		{"a trailing period", "it's on feedbad.example.", true},
		{"stacked punctuation", "see feedbad.example!!", true},
		{"a shouted scheme and mixed case host", "HTTPS://FeedBad.Example/p now", true},
		{"a port", "feedbad.example:8080/path ok", true},
		{"userinfo", "phish at https://user@feedbad.example/p today", true},
		{"a query string", "open feedbad.example/q?x=1 please", true},
		{"a www prefix", "visit www.feedbad.example now", true},
		{"an ellipsis", "wait... what...", false},
		{"a bare run of dots", "hmm ... ok", false},
		{"no dots", "no links here friends", false},
		{"a suffix collision", "notfeedbad.example is unrelated", false},
	}
	h := newHarness(t, nil, sinkholeDohbad)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, h.checker.Evaluate(tt.text, 42, "u1"))
		})
	}
}

func TestEvaluateNeverAsksTheOracleAboutNonHosts(t *testing.T) {
	h := newHarness(t, nil, sinkholeDohbad)

	h.checker.Evaluate("10.0.0.1 notahost a.b пример.рф evîl.example", 1, "u")
	h.settle(t)

	assert.Empty(t, h.doh.asked())
}

func TestEvaluateQueriesTheOracleOncePerRegistrableDomain(t *testing.T) {
	h := newHarness(t, nil, sinkholeDohbad)
	assert.False(t, h.checker.Evaluate("a.fine.example is fine folks", 1, "u"))
	h.eventually(t, func() bool { return len(h.doh.asked()) == 1 })

	for _, text := range []string{"a.fine.example is fine folks!", "b.c.fine.example is also fine", "fine.example again"} {
		assert.False(t, h.checker.Evaluate(text, 1, "u"), "a cached clean verdict must not flip to bad")
	}
	h.settle(t)

	assert.Equal(t, []string{"a.fine.example"}, h.doh.asked())
}

func TestEvaluateDoHConvictionLandsAsync(t *testing.T) {
	h := newHarness(t, nil, sinkholeDohbad)
	line := "dohbad.example is up go look"

	assert.False(t, h.checker.Evaluate(line, 7, "u2"), "an unknown host must not convict before any oracle answered")
	h.eventually(t, func() bool { return h.checker.Evaluate(line, 7, "u2") })

	select {
	case hit := <-h.hits:
		assert.Equal(t, Hit{Source: SourceDoH, Channel: 7, Sender: "u2"}, Hit{Source: hit.Source, Channel: hit.Channel, Sender: hit.Sender})
	default:
		t.Fatal("no Hit recorded for convicted host")
	}
}

func TestExpansionCarriesViaAndConvictsDestination(t *testing.T) {
	var destHits atomic.Int64
	dest := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destHits.Add(1) }))
	t.Cleanup(dest.Close)
	mid := redirectServer(t, "http://sdest.test/final")
	head := redirectServer(t, "http://smid.test/hop")
	exp := newExpanderScheme(
		clientFor(t, dnsMap{"shead.test": tok(head.URL), "smid.test": tok(mid.URL), "sdest.test": tok(dest.URL)}),
		[]string{"shead.test", "smid.test"}, "http")
	reply := func(_ int, host string) (int, string) {
		if host == "sdest.test" {
			return http.StatusOK, dohSinkhole
		}
		return http.StatusOK, dohClean
	}
	h := newHarness(t, exp, reply)
	token := "shead.test/abc"

	assert.False(t, h.checker.Evaluate(token, 9, "u3"), "a shortener token is unknown before expansion")
	h.eventually(t, func() bool { return h.checker.Evaluate(token, 9, "u3") })

	hit := <-h.hits
	assert.Equal(t, "shead.test", hit.Via)
	assert.Equal(t, "sdest.test", hit.Host)
	assert.Zero(t, destHits.Load(), "the destination is never contacted")
}

func TestVerdictsExpireAfterTheirRetention(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		advance       time.Duration
		wantConvicted bool
		wantQueries   int
	}{
		{"keeps a clean verdict inside its retention", "fine.example is fine", cleanTTL - time.Minute, false, 1},
		{"requeries a clean host after its retention", "fine.example is fine", cleanTTL + time.Minute, false, 2},
		{"keeps a bad verdict inside its retention", "dohbad.example is bad", badTTL - time.Minute, true, 1},
		{"requeries a bad host after its retention", "dohbad.example is bad", badTTL + time.Minute, false, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advance := skewClock(t)
			h := newHarness(t, nil, sinkholeDohbad)
			host := strings.Fields(tt.line)[0]
			h.checker.Evaluate(tt.line, 1, "u")
			h.eventually(t, func() bool { return len(h.doh.asked()) == 1 })
			h.settle(t)

			advance(tt.advance)
			convicted := h.checker.Evaluate(tt.line, 1, "u")
			h.settle(t)

			assert.Equal(t, tt.wantConvicted, convicted)
			assert.Equal(t, tt.wantQueries, h.queries(host))
		})
	}
}

func TestOracleOutageNeverCachesAndCoolsDown(t *testing.T) {
	advance := skewClock(t)
	outage := func(call int, host string) (int, string) {
		if host == "flaky.example" && call == 1 {
			return http.StatusServiceUnavailable, "down"
		}
		return http.StatusOK, dohClean
	}
	h := newHarness(t, nil, outage)
	line := "flaky.example is up"
	flakyQueries := func() int { return h.queries("flaky.example") }

	assert.False(t, h.checker.Evaluate(line, 1, "u"), "an outage must not convict")
	h.eventually(t, func() bool { return flakyQueries() == 1 })
	h.settle(t)
	assert.False(t, h.checker.Evaluate(line, 1, "u"))
	h.settle(t)
	assert.Equal(t, 1, flakyQueries(), "the host cools down after an outage")

	advance(errCooldown + time.Minute)
	h.checker.Evaluate(line, 1, "u")

	h.eventually(t, func() bool { return flakyQueries() == 2 })
}

func TestBadVerdictCacheStaysBoundedAndKeepsConvicting(t *testing.T) {
	c := NewChecker(Options{Workers: 1, DoH: NewDoH("http://127.0.0.1:1", nil)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	host := func(i int) string { return fmt.Sprintf("free-nitro-%d.example", i) }
	const batch = 200
	total := maxEntries + 2*batch

	for from := 0; from < total; from += batch {
		var line strings.Builder
		for i := from; i < from+batch; i++ {
			line.WriteString(host(i) + " ")
		}
		c.Evaluate(line.String(), 1, "u")
		last := host(from + batch - 1)
		require.Eventually(t, func() bool { return c.Evaluate(last, 1, "u") }, 5*time.Second, 100*time.Microsecond)
	}

	assert.True(t, c.Evaluate(host(total-1), 1, "u"), "the newest conviction is kept")
	assert.False(t, c.Evaluate(host(0), 1, "u"), "the oldest conviction is forgotten once the cache is full")
	require.Eventually(t, func() bool { return c.Evaluate(host(0), 1, "u") }, 5*time.Second, 100*time.Microsecond, "a forgotten host convicts again")
}

func TestCheckerDefaultsNeedNoConfiguration(t *testing.T) {
	assert.False(t, NewChecker(Options{}).Evaluate("unknown.example is up", 1, "u"))
}

func TestNilCheckerInert(t *testing.T) {
	var c *Checker

	assert.False(t, c.Evaluate("evil.example", 1, "u"))
}
