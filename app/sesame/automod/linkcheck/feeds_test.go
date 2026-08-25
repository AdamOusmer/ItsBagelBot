// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseFeedLines(t *testing.T) {
	cases := []struct {
		line   string
		format feedFormat
		want   string
	}{
		{"https://Phishy.Example/path?q=1", FormatLines, "phishy.example"},
		{"http://user@x.example/", FormatLines, "x.example"},
		{"bare.example/link", FormatLines, "bare.example"},
		{"evil.example:8443/p", FormatLines, "evil.example"},
		{"# comment", FormatLines, ""},
		{"", FormatLines, ""},
		{"/etc/passwd junk", FormatLines, ""},
		{"0.0.0.1 Evil.Hosts.Example", FormatHosts, "evil.hosts.example"},
		// Single-label entries fail validHost on purpose: they are junk or
		// search-engine-bait, never routable hosts worth a cache slot.
		{"127.0.0.1 onlyhost", FormatHosts, ""},
		{"# urlhaus comment", FormatHosts, ""},
	}
	for _, tt := range cases {
		if got := parseFeedLine([]byte(tt.line), tt.format); got != tt.want {
			t.Errorf("parseFeedLine(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestFeedsRefreshAndHas(t *testing.T) {
	lines := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("https://feedhit.example/one\nhttp://sub.two.example/two\n#note\n"))
	}))
	defer lines.Close()

	f := NewFeeds([]FeedSource{{Name: "test", URL: lines.URL, Format: FormatLines}}, nil)
	if f.Has("anything.example") {
		t.Fatal("unrefreshed feeds must answer empty")
	}

	n, err := f.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if n != 2 {
		t.Fatalf("installed %d hosts, want 2", n)
	}
	for _, h := range []string{"feedhit.example", "sub.two.example"} {
		if !f.Has(h) {
			t.Errorf("Has(%q) = false after refresh", h)
		}
	}
	// Parent walk: a QUERY's ancestors match listed entries, so subdomain
	// rotation of a LISTED host cannot dodge the snapshot. The inverse is
	// deliberately not true: a bare registrable query does not inherit a
	// listed subdomain's conviction - the walk goes up, never down.
	if !f.Has("rotating.feedhit.example") {
		t.Error("parent walk missed ancestor of listed host")
	}
	if f.Has("notfeedhit.example") {
		t.Error("suffix collision convicted an unrelated host")
	}
	if f.Has("example") {
		t.Error("tld-only probe convicted")
	}
}

func TestFeedsParentWalkDepthIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("listed.example/x\n"))
	}))
	defer srv.Close()

	f := NewFeeds([]FeedSource{{Name: "test", URL: srv.URL, Format: FormatLines}}, nil)
	if _, err := f.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// Four labels above the listed host exceeds the three-level walk: the
	// boundary is deliberate (deeper chains are CDN-shaped, not scam-shaped).
	if f.Has("a.b.c.d.listed.example") {
		t.Error("walk exceeded its documented depth bound")
	}
	if !f.Has("a.b.c.listed.example") {
		t.Error("three-level parent walk missed a listed ancestor")
	}
}

func TestFeedsTotalFailureKeepsPreviousSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("keepme.example/x\n"))
	}))
	url := srv.URL

	f := NewFeeds([]FeedSource{{Name: "test", URL: url, Format: FormatLines}}, nil)
	if _, err := f.Refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	srv.Close() // every subsequent pull fails
	if _, err := f.Refresh(context.Background()); err == nil {
		t.Fatal("expected total-failure error")
	}
	// The previous snapshot must keep serving through the outage.
	if !f.Has("keepme.example") {
		t.Error("failed refresh blanked the last good feed set")
	}
}
