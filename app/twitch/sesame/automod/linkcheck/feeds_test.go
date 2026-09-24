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
	requireListed(t, f, "feedhit.example", "listed host missing after refresh")
	requireListed(t, f, "sub.two.example", "listed host missing after refresh")
	requireListed(t, f, "rotating.feedhit.example", "parent walk missed ancestor of listed host")
	requireNotListed(t, f, "notfeedhit.example", "suffix collision convicted an unrelated host")
	requireNotListed(t, f, "example", "tld-only probe convicted")
}

func requireListed(t *testing.T, f *Feeds, host, msg string) {
	t.Helper()
	if !f.Has(host) {
		t.Errorf("%s: Has(%q) = false", msg, host)
	}
}

func requireNotListed(t *testing.T, f *Feeds, host, msg string) {
	t.Helper()
	if f.Has(host) {
		t.Errorf("%s: Has(%q) = true", msg, host)
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

	srv.Close()
	if _, err := f.Refresh(context.Background()); err == nil {
		t.Fatal("expected total-failure error")
	}
	if !f.Has("keepme.example") {
		t.Error("failed refresh blanked the last good feed set")
	}
}
