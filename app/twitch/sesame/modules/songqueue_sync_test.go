// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setSkipSnapshots(g *fakeGossip, before, after gossiprpc.SpotifyQueueReply) {
	g.sequences = map[string][]any{"spotify.playerqueue": {before, after}}
	g.replies["spotify.playerqueue"] = after
}

func spotifyQueue(current string, upNext ...string) gossiprpc.SpotifyQueueReply {
	reply := gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: current}}
	for _, id := range upNext {
		reply.UpNext = append(reply.UpNext, gossiprpc.SpotifyTrack{ID: id})
	}
	return reply
}

func TestSkipDrivesThePlayer(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{
		{TrackID: "t1", Title: "Human", Artists: []string{"The Killers"}, RequesterID: "42", RequesterName: "alice"},
	}}
	g := srSearchGossip()
	setSkipSnapshots(g, spotifyQueue("external", "t1"), spotifyQueue("t1"))
	m := SongQueue(songDeps(store, g))

	out := runChat(t, m, chatCtx("9", "mod", "moderator"), "!skip")

	require.NotEmpty(t, g.calls)
	assert.Equal(t, "next", g.calls[len(g.calls)-2].endpoint)
	assert.Contains(t, chatText(t, out), "Human")
	require.NotNil(t, store.current)
}

func TestSkip(t *testing.T) {
	human := entry("t1", "Human", "42", "alice")
	human.Artists = []string{"The Killers"}
	cases := []struct {
		name     string
		text     string
		store    []engine.SongEntry
		before   gossiprpc.SpotifyQueueReply
		after    gossiprpc.SpotifyQueueReply
		nextErr  string
		contains []string
		excludes []string
		current  string
		queued   int
	}{
		{name: "the skip command promotes the head request", text: "!skip", store: []engine.SongEntry{human},
			before: spotifyQueue("external", "t1"), after: spotifyQueue("t1"), contains: []string{"Human"}, current: "t1"},
		{name: "TestSRSkipVerbPromotesHead", text: "!sr skip", store: []engine.SongEntry{human},
			before: spotifyQueue("external", "t1"), after: spotifyQueue("t1"), contains: []string{"Human"}, current: "t1"},
		{name: "TestSkipWaitsForSpotifyBeforePromotingARequest", text: "!skip", store: []engine.SongEntry{entry("t1", "Human", "42", "alice")},
			before: spotifyQueue("external", "t1"), after: spotifyQueue("external", "t1"), contains: []string{"Skip sent"}, queued: 1},
		{name: "TestSkipAnnouncesTrackSpotifyActuallyReached", text: "!skip",
			store:  []engine.SongEntry{{TrackID: "missed", Title: "Missed"}, {TrackID: "actual", Title: "Actual", RequesterName: "bob"}},
			before: spotifyQueue("external", "missed", "actual"), after: spotifyQueue("actual"),
			contains: []string{"Actual"}, excludes: []string{"Missed"}, current: "actual"},
		{name: "TestSkipRefusalLeavesTheListAlone", text: "!skip", store: []engine.SongEntry{human}, nextErr: "Spotify Premium is required for queue control",
			before: spotifyQueue("external", "t1"), after: spotifyQueue("external", "t1"), contains: []string{"Premium"}, queued: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeSongQueue{up: tc.store}
			g := srSearchGossip()
			setSkipSnapshots(g, tc.before, tc.after)
			if tc.nextErr != "" {
				g.replies["spotify.next"] = gossiprpc.SpotifyPlayerReply{Error: tc.nextErr}
			}
			out := runChat(t, SongQueue(songDeps(store, g)), chatCtx("9", "mod", "moderator"), tc.text)
			assertText(t, chatText(t, out), textWant{"", tc.contains, tc.excludes})
			var endpoints []string
			for _, call := range g.calls {
				endpoints = append(endpoints, call.endpoint)
			}
			assert.Contains(t, endpoints, "next", "the player is asked to skip")
			assert.Len(t, store.up, tc.queued)
			if tc.current == "" {
				assert.Nil(t, store.current, "nothing may be marked played while the music kept playing")
				return
			}
			if assert.NotNil(t, store.current) {
				assert.Equal(t, tc.current, store.current.TrackID)
			}
		})
	}
}
