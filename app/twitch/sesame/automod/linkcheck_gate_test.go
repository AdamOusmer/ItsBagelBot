// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod/linkcheck"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"
)

// waitFor polls until cond holds; the checker resolves asynchronously by
// design, so first contact is allowed to come up empty.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition did not hold within 2s")
}

// blockingDoH serves a security resolver that sinks every name to 0.0.0.0.
func blockingDoH(t *testing.T) *linkcheck.DoH {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"0.0.0.0"}]}`))
	}))
	t.Cleanup(srv.Close)
	return linkcheck.NewDoH(srv.URL, nil)
}

// feedChecker arms the dynamic layer with a feed snapshot listing one host,
// giving the tests a SYNCHRONOUS conviction path (no network wait).
func feedChecker(t *testing.T) (*linkcheck.Checker, context.CancelFunc) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("convicted.example/scam\n"))
	}))
	t.Cleanup(srv.Close)

	feeds := linkcheck.NewFeeds([]linkcheck.FeedSource{{Name: "test", URL: srv.URL, Format: linkcheck.FormatLines}}, nil)
	if _, err := feeds.Refresh(context.Background()); err != nil {
		t.Fatalf("feed refresh: %v", err)
	}
	c := linkcheck.NewChecker(linkcheck.Options{
		Feeds: feeds,
		DoH:   blockingDoH(t),
	})
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	return c, cancel
}

func TestGatePhishVerdictOnFeedListedHost(t *testing.T) {
	c, stop := feedChecker(t)
	defer stop()

	g := New()
	g.SetLinkChecker(c)

	line := "see convicted.example ok friends"
	v := g.InspectWith(module.RoleEveryone, line, nil)
	if want := (Verdict{Action: ActionTimeout, Seconds: 600, Rule: "phish"}); v != want {
		t.Fatalf("verdict = %+v, want %+v", v, want)
	}
}

func TestGateUnknownHostResolvesAsyncThenConvicts(t *testing.T) {
	c := linkcheck.NewChecker(linkcheck.Options{
		Feeds: linkcheck.NewFeeds(nil, nil),
		DoH:   blockingDoH(t),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)

	g := New()
	g.SetLinkChecker(c)

	line := "doomed.example is live go look"
	if v := g.InspectWith(module.RoleEveryone, line, nil); v.Action != ActionNone {
		t.Fatalf("first sight convicted before any oracle ran: %+v", v)
	}
	waitFor(t, func() bool {
		return g.InspectWith(module.RoleEveryone, line, nil).Action == ActionTimeout
	})
}

func TestGateFloorStillWinsOverLinkCheck(t *testing.T) {
	c, stop := feedChecker(t)
	defer stop()

	g := New()
	g.SetLinkChecker(c)

	// grabify.link is immovable-floor infrastructure; the phish layer must not
	// re-badge it even though the checker's oracle would also convict.
	if v := g.InspectWith(module.RoleEveryone, "visit grabify.link now", nil); v.Rule != "ip_logger" {
		t.Fatalf("rule = %q, want ip_logger (floor precedence)", v.Rule)
	}
}

func TestGateLinksOffProfileSkipsLinkCheck(t *testing.T) {
	c, stop := feedChecker(t)
	defer stop()

	g := New()
	g.SetLinkChecker(c)

	cfg := ParseConfig(codec.RawMessage(`{"level":"none"}`))
	if v := g.InspectWith(module.RoleEveryone, "see convicted.example ok", cfg); v.Action != ActionNone {
		t.Fatalf("floor-only profile judged links: %+v", v)
	}
}

func TestGateUnarmedStaysInert(t *testing.T) {
	g := New() // no SetLinkChecker: byte-identical to the pre-linkcheck gate
	if v := g.InspectWith(module.RoleEveryone, "see convicted.example ok", nil); v.Action != ActionNone {
		t.Fatalf("unarmed gate acted: %+v", v)
	}
}
