// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond until it holds or the deadline lapses, because the
// checker resolves asynchronously by design.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// harness wires a checker against httptest-backed oracles: a one-line feed and
// a security resolver that blocks everything. Tests needing different answers
// build their own oracle inline.
type harness struct {
	checker *Checker
	hits    chan Hit
}

func newHarness(t *testing.T, expander *Expander) *harness {
	t.Helper()

	feedsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("feedbad.example/scam\n"))
	}))
	t.Cleanup(feedsSrv.Close)

	doh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"0.0.0.0"}]}`))
	}))
	t.Cleanup(doh.Close)

	feeds := NewFeeds([]FeedSource{{Name: "test", URL: feedsSrv.URL, Format: FormatLines}}, nil)
	if _, err := feeds.Refresh(context.Background()); err != nil {
		t.Fatalf("feed refresh: %v", err)
	}

	h := &harness{hits: make(chan Hit, 16)}
	h.checker = NewChecker(Options{
		ExpandShorteners: true,
		Workers:          1,
		Feeds:            feeds,
		DoH:              NewDoH(doh.URL, nil),
		Expander:         expander,
	})
	h.checker.OnBad = func(hit Hit) { h.hits <- hit }

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.checker.Start(ctx)
	return h
}

func TestEvaluateFeedHitIsImmediate(t *testing.T) {
	h := newHarness(t, nil)
	if !h.checker.Evaluate("everyone check feedbad.example/scam now", 42, "u1") {
		t.Fatal("feed-listed host must convict on the first synchronous pass")
	}
}

func TestEvaluateDoHConvictionLandsAsync(t *testing.T) {
	h := newHarness(t, nil)
	line := "dohbad.example is up go look"
	if h.checker.Evaluate(line, 7, "u2") {
		t.Fatal("unknown host must not convict before any oracle answered")
	}
	if !waitFor(t, func() bool { return h.checker.Evaluate(line, 7, "u2") }) {
		t.Fatal("doh conviction never landed")
	}
	select {
	case hit := <-h.hits:
		if hit.Source != SourceDoH || hit.Channel != 7 || hit.Sender != "u2" {
			t.Fatalf("hit = %+v, want doh conviction carrying enqueue context", hit)
		}
	default:
		t.Fatal("no Hit recorded for convicted host")
	}
}

func TestExpansionCarriesViaAndConvictsDestination(t *testing.T) {
	var destHits atomic.Int64
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destHits.Add(1)
	}))
	t.Cleanup(dest.Close)
	mid := redirectServer(t, "http://sdest.test/final")
	head := redirectServer(t, "http://smid.test/hop")

	exp := newExpanderScheme(
		clientFor(t, dnsMap{
			"shead.test": tok(head.URL),
			"smid.test":  tok(mid.URL),
			"sdest.test": tok(dest.URL),
		}),
		[]string{"shead.test", "smid.test"}, "http")

	h := newHarness(t, exp)
	token := "shead.test/abc"

	if h.checker.Evaluate(token, 9, "u3") {
		t.Fatal("shortener token unknown before expansion")
	}
	if !waitFor(t, func() bool { return h.checker.Evaluate(token, 9, "u3") }) {
		t.Fatal("expansion conviction never landed")
	}

	hit := <-h.hits
	if hit.Via != "shead.test" || hit.Host != "sdest.test" {
		t.Fatalf("hit via=%q host=%q, want via=shead.test host=sdest.test", hit.Via, hit.Host)
	}
	if destHits.Load() != 0 {
		t.Fatalf("destination contacted %d times; contract is never", destHits.Load())
	}
}

func TestCleanVerdictSuppressesRepeatEnqueues(t *testing.T) {
	var oracleCalls int
	cleanDoh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		oracleCalls++
		_, _ = w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"1.2.3.4"}]}`))
	}))
	defer cleanDoh.Close()

	c := NewChecker(Options{
		Workers: 1,
		Feeds:   NewFeeds(nil, nil),
		DoH:     NewDoH(cleanDoh.URL, nil),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	line := "fine.example is fine folks"
	if c.Evaluate(line, 1, "u") {
		t.Fatal("clean oracle convicted")
	}
	key := foldHost("fine.example")
	if !waitFor(t, func() bool { _, ok := c.cache.get(key); return ok }) {
		t.Fatal("clean verdict never cached")
	}
	queried := oracleCalls

	for i := 0; i < 20; i++ {
		if c.Evaluate(line+"!", 1, "u") {
			t.Fatal("cached Clean flipped to bad")
		}
	}
	if oracleCalls != queried {
		t.Fatalf("repeat Evaluate re-queried the oracle %d extra times", oracleCalls-queried)
	}
}

func TestOracleOutageNeverCachesAndCoolsDown(t *testing.T) {
	deadURL := ""
	{
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		deadURL = srv.URL
		srv.Close()
	}
	feeds := NewFeeds(nil, nil)
	c := NewChecker(Options{Workers: 1, Feeds: feeds, DoH: NewDoH(deadURL, nil)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	pinTime(t)
	line := "flaky.example is up"
	if c.Evaluate(line, 1, "u") {
		t.Fatal("outage convicted a host")
	}
	key := foldHost("flaky.example")
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.cooldown[key] > nowNanos()
	})
	if _, ok := c.cache.get(key); ok {
		t.Fatal("oracle outage cached an answer")
	}
}

func TestNilCheckerInert(t *testing.T) {
	var c *Checker
	if c.Evaluate("evil.example", 1, "u") {
		t.Fatal("nil checker convicted")
	}
}
