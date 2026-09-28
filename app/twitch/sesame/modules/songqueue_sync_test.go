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

func skipReadyGossip() *fakeGossip {
	g := srSearchGossip()
	setSkipSnapshots(g,
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "external"}, UpNext: []gossiprpc.SpotifyTrack{{ID: "t1"}}},
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "t1"}})
	return g
}

func TestSkipDrivesThePlayer(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{
		{TrackID: "t1", Title: "Human", Artists: []string{"The Killers"}, RequesterID: "42", RequesterName: "alice"},
	}}
	g := skipReadyGossip()
	m := SongQueue(songDeps(store, g))

	out := runSongCmd(t, m, "skip", songCtx("9", "mod", "moderator"))

	require.NotEmpty(t, g.calls)
	assert.Equal(t, "next", g.calls[len(g.calls)-2].endpoint)
	assert.Contains(t, chatText(t, out), "Human")
	require.NotNil(t, store.current)
}

func TestSkipWaitsForSpotifyBeforePromotingARequest(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{{TrackID: "t1", Title: "Human", RequesterID: "42", RequesterName: "alice"}}}
	g := srSearchGossip()
	unchanged := gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "external"}, UpNext: []gossiprpc.SpotifyTrack{{ID: "t1"}}}
	setSkipSnapshots(g, unchanged, unchanged)
	m := SongQueue(songDeps(store, g))

	out := runSongCmd(t, m, "skip", songCtx("9", "mod", "moderator"))
	assert.Contains(t, chatText(t, out), "Skip sent")
	assert.Nil(t, store.current)
	assert.Len(t, store.up, 1)
}

func TestSkipAnnouncesTrackSpotifyActuallyReached(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{
		{TrackID: "missed", Title: "Missed"},
		{TrackID: "actual", Title: "Actual", RequesterName: "bob"},
	}}
	g := srSearchGossip()
	setSkipSnapshots(g,
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "external"}, UpNext: []gossiprpc.SpotifyTrack{{ID: "missed"}, {ID: "actual"}}},
		gossiprpc.SpotifyQueueReply{Current: &gossiprpc.SpotifyTrack{ID: "actual"}})
	m := SongQueue(songDeps(store, g))

	out := runSongCmd(t, m, "skip", songCtx("9", "mod", "moderator"))
	assert.Contains(t, chatText(t, out), "Actual")
	assert.NotContains(t, chatText(t, out), "Missed")
	require.NotNil(t, store.current)
	assert.Equal(t, "actual", store.current.TrackID)
	assert.Empty(t, store.up)
}

func TestSkipRefusalLeavesTheListAlone(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{
		{TrackID: "t1", Title: "Human", Artists: []string{"The Killers"}, RequesterID: "42", RequesterName: "alice"},
	}}
	g := srSearchGossip()
	g.replies["spotify.next"] = gossiprpc.SpotifyPlayerReply{Error: "Spotify Premium is required for queue control"}
	m := SongQueue(songDeps(store, g))

	out := runSongCmd(t, m, "skip", songCtx("9", "mod", "moderator"))

	assert.Contains(t, chatText(t, out), "Premium")
	assert.Nil(t, store.current, "nothing may be marked played while the music kept playing")
	assert.Len(t, store.up, 1)
}

func TestSRViewRenumbersAfterSpotifySkippedPendingTrack(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{
		{TrackID: "a", Title: "Skipped", RequesterName: "alice"},
		{TrackID: "b", Title: "Survivor", RequesterName: "bob"},
	}}
	g := srSearchGossip()
	g.replies["spotify.playerqueue"] = gossiprpc.SpotifyQueueReply{
		Current: &gossiprpc.SpotifyTrack{ID: "external"},
		UpNext:  []gossiprpc.SpotifyTrack{{ID: "b"}},
	}
	m := SongQueue(songDeps(store, g))

	out := runSR(t, m, songCtx("9", "viewer"), "")
	text := chatText(t, out)
	assert.Contains(t, text, "1. Survivor (by bob)")
	assert.NotContains(t, text, "Skipped")
}
