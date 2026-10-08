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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blockingDoH(t *testing.T) *linkcheck.DoH {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"0.0.0.0"}]}`))
	}))
	t.Cleanup(srv.Close)
	return linkcheck.NewDoH(srv.URL, nil)
}

func startChecker(t *testing.T, feeds *linkcheck.Feeds) *linkcheck.Checker {
	t.Helper()
	c := linkcheck.NewChecker(linkcheck.Options{Feeds: feeds, DoH: blockingDoH(t)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Start(ctx)
	return c
}

func feedChecker(t *testing.T) *linkcheck.Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("convicted.example/scam\n"))
	}))
	t.Cleanup(srv.Close)
	feeds := linkcheck.NewFeeds([]linkcheck.FeedSource{{Name: "test", URL: srv.URL, Format: linkcheck.FormatLines}}, nil)
	_, err := feeds.Refresh(context.Background())
	require.NoError(t, err)
	return startChecker(t, feeds)
}

func TestGateLinkCheck(t *testing.T) {
	tests := []struct {
		name  string
		armed bool
		cfg   string
		line  string
		want  Verdict
	}{
		{"convicts a feed-listed host with a phish verdict", true, "", "see convicted.example ok friends", Verdict{Action: ActionTimeout, Seconds: 600, Rule: "phish"}},
		{"lets the floor win over the link check", true, "", "visit grabify.link now", verdictIPLogger},
		{"skips the link check under a floor-only level", true, `{"level":"none"}`, "see convicted.example ok", Verdict{}},
		{"stays inert when unarmed", false, "", "see convicted.example ok", Verdict{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New()
			if tt.armed {
				g.SetLinkChecker(feedChecker(t))
			}

			assert.Equal(t, tt.want, g.InspectWith(module.RoleEveryone, tt.line, ParseConfig(codec.RawMessage(tt.cfg))))
		})
	}
}

func TestGateUnknownHostResolvesAsyncThenConvicts(t *testing.T) {
	g := New()
	g.SetLinkChecker(startChecker(t, linkcheck.NewFeeds(nil, nil)))
	line := "doomed.example is live go look"

	assert.Equal(t, Verdict{}, g.Inspect(module.RoleEveryone, line), "first sight convicts before any oracle ran")

	require.Eventually(t, func() bool {
		return g.Inspect(module.RoleEveryone, line).Action == ActionTimeout
	}, 2*time.Second, 2*time.Millisecond)
}
