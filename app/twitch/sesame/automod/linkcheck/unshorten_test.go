// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func redirectServer(t *testing.T, location string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if location == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, location, http.StatusMovedPermanently)
	}))
	t.Cleanup(s.Close)
	return s
}

func tok(url string) string { return trimLinkToken(url) }

type dnsMap map[string]string

func clientFor(t *testing.T, m dnsMap) *http.Client {
	t.Helper()
	return &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				real, ok := m[strings.SplitN(addr, ":", 2)[0]]
				if !ok {
					return nil, fmt.Errorf("fake DNS: %q not mapped", addr)
				}
				var d net.Dialer
				return d.DialContext(ctx, network, real)
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func TestDestinationStopsAtAllowlistBoundary(t *testing.T) {
	var destHits atomic.Int64
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destHits.Add(1)
	}))
	defer dest.Close()

	mid := redirectServer(t, "http://sdest.test/final")
	head := redirectServer(t, "http://smid.test/m")

	e := newExpanderScheme(
		clientFor(t, dnsMap{
			"shead.test": tok(head.URL),
			"smid.test":  tok(mid.URL),
			"sdest.test": tok(dest.URL),
		}),
		[]string{"shead.test", "smid.test"}, "http")

	got, err := e.Destination(context.Background(), "shead.test/abc")
	if err != nil {
		t.Fatalf("destination: %v", err)
	}
	if got != "sdest.test" {
		t.Fatalf("destination = %q, want sdest.test", got)
	}
	if destHits.Load() != 0 {
		t.Fatalf("destination was contacted %d times; contract is never", destHits.Load())
	}
}

func TestDestinationNonShortenerInputIsUntouched(t *testing.T) {
	e := newExpanderScheme(testClient(), []string{"bit.ly"}, "http")
	got, err := e.Destination(context.Background(), "plain.example/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain.example" {
		t.Fatalf("got %q, want plain.example returned uncontacted", got)
	}
}

func TestDestinationInterstitialReturnsShortenerItself(t *testing.T) {
	wall := redirectServer(t, "")
	e := newExpanderScheme(clientFor(t, dnsMap{"swall.test": tok(wall.URL)}), []string{"swall.test"}, "http")
	got, err := e.Destination(context.Background(), "swall.test/xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "swall.test" {
		t.Fatalf("interstitial destination = %q, want the shortener host swall.test", got)
	}
}

func TestDestinationLoopCappedAtHops(t *testing.T) {
	loop := redirectServer(t, "/self")
	e := newExpanderScheme(clientFor(t, dnsMap{"sloop.test": tok(loop.URL)}), []string{"sloop.test"}, "http")
	if _, err := e.Destination(context.Background(), "sloop.test/start"); err == nil {
		t.Fatal("redirect loop resolved without error")
	}
}

func TestDestinationRefusesExoticScheme(t *testing.T) {
	srv := redirectServer(t, "javascript:alert(1)")
	e := newExpanderScheme(clientFor(t, dnsMap{"sscheme.test": tok(srv.URL)}), []string{"sscheme.test"}, "http")
	if _, err := e.Destination(context.Background(), "sscheme.test/x"); err == nil {
		t.Fatal("non-http scheme accepted")
	}
}

func TestGuardDial(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
	}{
		{"8.8.8.8:443", true},
		{"1.1.1.1:53", true},
		{"2606:4700::1111", false},
		{"127.0.0.1:8080", false},
		{"10.1.2.3:443", false},
		{"192.168.0.20:443", false},
		{"172.16.9.9:443", false},
		{"100.64.0.9:443", false},
		{"169.254.169.254:80", false},
		{"[fd00::5]:443", false},
		{"224.0.0.1:5353", false},
		{"not-an-address:443", false},
	}
	for _, tt := range cases {
		err := guardDial("tcp", tt.addr, nil)
		if tt.ok && err != nil {
			t.Errorf("guardDial(%q) = %v, want allowed", tt.addr, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("guardDial(%q) allowed, want refused", tt.addr)
		}
	}
}

func TestGuardDialBlocksLiveLoopbackDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	e := NewExpander(nil, []string{hostOf(tok(srv.URL))})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if _, err := e.client.Do(req); err == nil {
		t.Fatal("loopback dial succeeded despite guardDial")
	}
}
