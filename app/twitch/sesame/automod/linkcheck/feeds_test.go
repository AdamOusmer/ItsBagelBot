// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func feedServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFeedsRefreshInstallsEveryListedHost(t *testing.T) {
	lines := feedServer(t, "https://Phishy.Example/path?q=1\n"+
		"http://user@x.example/\n"+
		"bare.example/link\n"+
		"evil.example:8443/p\n"+
		"listed.example/x\n"+
		"# comment\n\n"+
		"/etc/passwd junk\n")
	hosts := feedServer(t, "0.0.0.1 Evil.Hosts.Example\n127.0.0.1 onlyhost\n# urlhaus comment\n")
	f := NewFeeds([]FeedSource{
		{Name: "lines", URL: lines.URL, Format: FormatLines},
		{Name: "hosts", URL: hosts.URL, Format: FormatHosts},
	}, nil)
	assert.False(t, f.Has("phishy.example"), "unrefreshed feeds answer empty")

	n, err := f.Refresh(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 6, n)
	for _, host := range []string{"phishy.example", "x.example", "bare.example", "evil.example", "evil.hosts.example", "rotating.phishy.example", "a.b.c.listed.example"} {
		assert.True(t, f.Has(host), host)
	}
	for _, host := range []string{"notphishy.example", "example", "onlyhost", "etc", "a.b.c.d.listed.example"} {
		assert.False(t, f.Has(host), host)
	}
}

func TestFeedsTotalFailureKeepsPreviousSet(t *testing.T) {
	srv := feedServer(t, "keepme.example/x\n")
	f := NewFeeds([]FeedSource{{Name: "test", URL: srv.URL, Format: FormatLines}}, nil)
	_, err := f.Refresh(context.Background())
	require.NoError(t, err)

	srv.Close()
	_, err = f.Refresh(context.Background())

	require.Error(t, err)
	assert.True(t, f.Has("keepme.example"), "a failed refresh must not blank the last good feed set")
}
