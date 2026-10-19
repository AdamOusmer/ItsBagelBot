// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeaderboardLink(t *testing.T) {
	standings := []topViewer{{id: "8", login: "alpha", name: "Alpha", points: 9000}}
	cases := []struct {
		name     string
		proj     *fakeProj
		baseURL  string
		noLogin  bool
		contains []string
		excludes []string
	}{
		{name: "visible page adds the link", proj: &fakeProj{}, contains: []string{"1. Alpha 9000", "https://leaderboard.itsbagelbot.com/streamer"}},
		{name: "configured base is used without a double slash", proj: &fakeProj{}, baseURL: "https://staging.example.com/",
			contains: []string{"https://staging.example.com/streamer"}, excludes: []string{"example.com//"}},
		{name: "missing login falls back to the id", proj: &fakeProj{}, noLogin: true, contains: []string{"leaderboard.itsbagelbot.com/2"}},
		{name: "hidden page replies without a link", proj: &fakeProj{user: projection.User{LeaderboardPageHidden: true}},
			contains: []string{"1. Alpha 9000"}, excludes: []string{"http"}},
		{name: "a hidden commands page does not hide the leaderboard link", proj: &fakeProj{user: projection.User{CommandsPageHidden: true}},
			contains: []string{"leaderboard.itsbagelbot.com/streamer"}},
		{name: "fails open when the projection errors", proj: &fakeProj{userErr: errors.New("projection unavailable")},
			contains: []string{"leaderboard.itsbagelbot.com/streamer"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := loyaltyDeps(&fakeLoyalty{topViewers: standings})
			d.Proj = tc.proj
			d.LeaderboardBaseURL = tc.baseURL
			c := loyaltyChat(viewer, "")
			c.Env.BroadcasterUserLogin = "Streamer"
			if tc.noLogin {
				c.Env.BroadcasterUserLogin = ""
			}
			out := runChat(t, Loyalty(d), c, "!leaderboard")
			require.Len(t, out, 1)
			for _, want := range tc.contains {
				assert.Contains(t, out[0].Text, want)
			}
			for _, bad := range tc.excludes {
				assert.NotContains(t, out[0].Text, bad)
			}
		})
	}
}

func TestLeaderboardLinkSurvivesTenLongNames(t *testing.T) {
	var standings []topViewer
	for i := range 10 {
		standings = append(standings, topViewer{id: fmt.Sprint(i + 8), login: strings.Repeat("n", 25), points: 9223372036854775807})
	}
	d := loyaltyDeps(&fakeLoyalty{topViewers: standings})
	d.Proj = &fakeProj{}
	c := loyaltyChat(viewer, "")
	c.Env.BroadcasterUserLogin = "streamer"
	out := runChat(t, Loyalty(d), c, "!leaderboard 10")
	require.Len(t, out, 1)
	assert.LessOrEqual(t, len([]rune(out[0].Text)), 500)
	assert.True(t, strings.HasSuffix(out[0].Text, "https://leaderboard.itsbagelbot.com/streamer"))
	assert.Contains(t, out[0].Text, "1. ")
}
