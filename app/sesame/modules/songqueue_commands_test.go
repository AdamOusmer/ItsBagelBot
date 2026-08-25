// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The standalone !song / !current / !skip spellings. Kept out of
// songqueue_test.go, which already carries the add, retract, remove and view
// behaviour of !sr itself.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runSong invokes the standalone !song / !current command.
func runSong(t *testing.T, m module.Module, c *module.Context) []module.Output {
	t.Helper()
	cmd := findCmd(t, m, "song")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), c, "", col.emit))
	return col.out
}

// nowPlayingGossip fakes the live player read behind !current.
func nowPlayingGossip(reply gossiprpc.SpotifyNowPlayingReply) *fakeGossip {
	return &fakeGossip{replies: map[string]any{"spotify.nowplaying": reply}}
}

func playing(tr gossiprpc.SpotifyTrack) gossiprpc.SpotifyNowPlayingReply {
	return gossiprpc.SpotifyNowPlayingReply{IsPlaying: true, Track: &tr}
}

func TestSongCommandFallsBackToTheQueueWhenNothingPlays(t *testing.T) {
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, nowPlayingGossip(gossiprpc.SpotifyNowPlayingReply{})))

	out := runSong(t, m, songCtx("42", "alice"))

	assert.Contains(t, chatText(t, out), "Nothing queued", "an idle player degrades to the queue view")
}

// The whole point of the command: it answers about whatever is audible, which
// on most channels is the broadcaster's own playlist rather than a request.
func TestCurrentReadsTheLivePlayerNotTheQueue(t *testing.T) {
	g := nowPlayingGossip(playing(srTrack("t9", "Unrequested", "Some Artist")))
	m := SongQueue(songDeps(&fakeSongQueue{}, g))

	out := runSong(t, m, songCtx("42", "alice"))

	call := g.lastCall(t)
	assert.Equal(t, "spotify", call.provider)
	assert.Equal(t, "nowplaying", call.endpoint)
	assert.Equal(t, "100", call.req.ChannelID, "the broadcaster id scopes gossip's per-channel credential")

	text := chatText(t, out)
	assert.Contains(t, text, "Unrequested")
	assert.Contains(t, text, "Some Artist")
	assert.NotContains(t, text, "Nothing queued")
}

// A track that IS the queue head gets its requester credited; one that is not
// must never borrow the credit of whoever happens to be waiting.
func TestCurrentCreditsOnlyTheMatchingRequester(t *testing.T) {
	store := &fakeSongQueue{current: &engine.SongEntry{
		TrackID: "t1", Title: "One", Artists: []string{"A"}, RequesterID: "7", RequesterName: "alice",
	}}
	m := SongQueue(songDeps(store, nowPlayingGossip(playing(srTrack("t1", "One", "A")))))
	assert.Contains(t, chatText(t, runSong(t, m, songCtx("42", "bob"))), "alice")

	other := SongQueue(songDeps(store, nowPlayingGossip(playing(srTrack("t2", "Two", "B")))))
	text := chatText(t, runSong(t, other, songCtx("42", "bob")))
	assert.Contains(t, text, "Two")
	assert.NotContains(t, text, "alice", "a broadcaster-started track has no requester to credit")
}

func TestCurrentSurfacesProviderReason(t *testing.T) {
	g := nowPlayingGossip(gossiprpc.SpotifyNowPlayingReply{Error: "no Spotify app set up for this channel"})
	m := SongQueue(songDeps(&fakeSongQueue{}, g))

	assert.Contains(t, chatText(t, runSong(t, m, songCtx("42", "alice"))), "no Spotify app set up")
}

func TestCurrentTransportErrorStaysGeneric(t *testing.T) {
	m := SongQueue(songDeps(&fakeSongQueue{}, &fakeGossip{err: errors.New("connection reset")}))

	assert.Contains(t, chatText(t, runSong(t, m, songCtx("42", "alice"))), "music lookup is down")
}

func TestCurrentUsesBroadcasterTemplateOverride(t *testing.T) {
	m := SongQueue(songDeps(&fakeSongQueue{}, nowPlayingGossip(playing(srTrack("t1", "One", "A")))))

	c := songCtx("42", "alice")
	c.Config = []byte(`{"currentMessage":"jamming to {title} right now"}`)

	assert.Equal(t, "jamming to One right now", chatText(t, runSong(t, m, c)))
}

func TestSongCommandCarriesTheSpokenAliases(t *testing.T) {
	m := SongQueue(songDeps(&fakeSongQueue{}, srSearchGossip()))
	cmd := findCmd(t, m, "song")
	assert.Contains(t, cmd.Aliases, "current")
	assert.Contains(t, cmd.Aliases, "nowplaying")
	assert.Contains(t, cmd.Aliases, "np")
}

// !skip carries the moderator gate on the command itself, where !sr next has to
// enforce it by hand because a bare word there could also be a song title.
func TestSkipCommandIsModeratorOnly(t *testing.T) {
	m := SongQueue(songDeps(&fakeSongQueue{}, srSearchGossip()))
	cmd := findCmd(t, m, "skip")
	assert.Equal(t, module.RoleModerator, cmd.Perm)
}

func TestSkipCommandPromotesHead(t *testing.T) {
	store := &fakeSongQueue{up: []engine.SongEntry{
		{TrackID: "t1", Title: "Human", Artists: []string{"The Killers"}, RequesterID: "42", RequesterName: "alice"},
	}}
	m := SongQueue(songDeps(store, srSearchGossip()))

	cmd := findCmd(t, m, "skip")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), songCtx("9", "mod", "moderator"), "", col.emit))

	assert.Contains(t, chatText(t, col.out), "Human")
}

// The viewer queue module owns !queue, and command precedence is registration
// order in All(). Claiming it from songqueue would shadow a different feature
// on any channel running both, so this pins that it stays unclaimed here.
func TestSongQueueDoesNotClaimQueueCommand(t *testing.T) {
	m := SongQueue(songDeps(&fakeSongQueue{}, srSearchGossip()))
	for _, c := range m.Commands {
		assert.NotEqual(t, "queue", c.Name, "!queue belongs to the viewer queue module")
		assert.NotContains(t, c.Aliases, "queue", "!queue belongs to the viewer queue module")
	}
}
