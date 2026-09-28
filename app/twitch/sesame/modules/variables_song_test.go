// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type variableSnapshotStore struct {
	engine.SongQueueStore
	current *engine.SongEntry
	err     error
	reads   int
}

func (s *variableSnapshotStore) Snapshot(context.Context, uint64, int) (engine.SongQueueSnapshot, error) {
	s.reads++
	return engine.SongQueueSnapshot{Current: s.current}, s.err
}

func (s *variableSnapshotStore) SyncPlaying(context.Context, uint64, string) (bool, error) {
	return false, nil
}

func (s *variableSnapshotStore) SyncQueue(context.Context, uint64, engine.PlayerQueueIDs) (bool, error) {
	return false, nil
}

type songVariableCase struct {
	name                     string
	reply                    gossiprpc.SpotifyNowPlayingReply
	player                   *gossiprpc.SpotifyQueueReply
	current                  *engine.SongEntry
	gossipErr, snapshotErr   error
	wantTitle, wantRequester string
	wantErr                  error
	wantReads                int
	noGossip                 bool
}

func TestSongVariablesPreservePlaybackSourceAndFailurePrecedence(t *testing.T) {
	track := &gossiprpc.SpotifyTrack{ID: "live", Name: "Live song", Artists: []string{"Artist"}, URL: "live-url"}
	queued := &engine.SongEntry{TrackID: "live", Title: "Queued song", Artists: []string{"Queued artist"}, RequesterName: "Alice", URL: "queued-url"}
	failure := errors.New("source failed")
	for _, tc := range []songVariableCase{
		{name: "playing track with matched request", reply: gossiprpc.SpotifyNowPlayingReply{IsPlaying: true, Track: track}, current: queued, wantTitle: "Live song", wantRequester: "Alice", wantReads: 1},
		{name: "playing track survives queue error", reply: gossiprpc.SpotifyNowPlayingReply{IsPlaying: true, Track: track}, snapshotErr: failure, wantTitle: "Live song", wantReads: 1},
		{name: "stopped player falls back to queue", reply: gossiprpc.SpotifyNowPlayingReply{Track: track}, current: queued, wantTitle: "Queued song", wantRequester: "Alice", wantReads: 1},
		{name: "queue works without player wiring", noGossip: true, current: queued, wantTitle: "Queued song", wantRequester: "Alice", wantReads: 1},
		{name: "provider refusal skips queue", reply: gossiprpc.SpotifyNowPlayingReply{Error: "not linked"}, current: queued},
		{name: "transport failure skips queue", gossipErr: failure, current: queued},
		{name: "fresh player queue is the source", player: &gossiprpc.SpotifyQueueReply{Current: track}, reply: gossiprpc.SpotifyNowPlayingReply{IsPlaying: true, Track: &gossiprpc.SpotifyTrack{ID: "stale", Name: "Stale song"}}, current: queued, wantTitle: "Live song", wantRequester: "Alice", wantReads: 1},
		{name: "idle fresh player falls back to queue", player: &gossiprpc.SpotifyQueueReply{}, reply: gossiprpc.SpotifyNowPlayingReply{IsPlaying: true, Track: track}, current: queued, wantTitle: "Queued song", wantRequester: "Alice", wantReads: 1},
		{name: "upstream refusal never becomes a value", reply: gossiprpc.SpotifyNowPlayingReply{Error: "Spotify Premium is required for queue control"}, current: queued},
		{name: "queue failure with no playback propagates", snapshotErr: failure, wantErr: failure, wantReads: 1},
		{name: "unrelated request stays anonymous", reply: gossiprpc.SpotifyNowPlayingReply{IsPlaying: true, Track: track}, current: &engine.SongEntry{TrackID: "other", RequesterName: "Bob"}, wantTitle: "Live song", wantReads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) { assertSongVariableCase(t, tc) })
	}
}

func assertSongVariableCase(t *testing.T, tc songVariableCase) {
	t.Helper()
	queue := &variableSnapshotStore{current: tc.current, err: tc.snapshotErr}
	deps := engine.Deps{SongQueue: queue}
	if !tc.noGossip {
		replies := map[string]any{"spotify.nowplaying": tc.reply}
		if tc.player != nil {
			replies["spotify.playerqueue"] = *tc.player
		}
		deps.Gossip = &fakeGossip{replies: replies, err: tc.gossipErr}
	}
	values, err := songVariables(deps)(context.Background(), urchinCtx(""))
	assert.ErrorIs(t, err, tc.wantErr)
	assert.Equal(t, tc.wantTitle, values["title"])
	assert.Equal(t, tc.wantRequester, values["req"])
	assert.Equal(t, tc.wantReads, queue.reads)
	assert.NotContains(t, values["song"], "{")
}

func runSongVariableTemplate(t *testing.T, gossip *fakeGossip, store *fakeSongQueue) string {
	t.Helper()
	response := "{songqueue:song} | {songqueue:title} | {songqueue:artist} | {songqueue:req}"
	views := []projection.ModuleView{enabledVariables("songqueue", `{}`)}
	return runVariableCommand(t, response, views, gossip, func(d *engine.Deps) { d.SongQueue = store })
}

func advancedSongQueue() *fakeSongQueue {
	return &fakeSongQueue{
		current: &engine.SongEntry{TrackID: "t0", Title: "Finished", RequesterName: "Bob"},
		up:      []engine.SongEntry{{TrackID: "t1", Title: "Human", Artists: []string{"The Killers"}, RequesterName: "Alice"}},
	}
}

func TestSongVariablesReadThePlayerOncePerRender(t *testing.T) {
	human := srTrack("t1", "Human", "The Killers")
	for _, tc := range []struct {
		name      string
		replies   map[string]any
		wantCalls []string
	}{
		{name: "player queue", replies: map[string]any{"spotify.playerqueue": gossiprpc.SpotifyQueueReply{Current: &human}}, wantCalls: []string{"playerqueue"}},
		{name: "now playing fallback", replies: map[string]any{"spotify.nowplaying": playing(human)}, wantCalls: []string{"playerqueue", "nowplaying"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gossip := &fakeGossip{replies: tc.replies}
			store := advancedSongQueue()
			assert.Equal(t, "Human by The Killers | Human | The Killers | Alice", runSongVariableTemplate(t, gossip, store))
			var endpoints []string
			for _, call := range gossip.calls {
				endpoints = append(endpoints, call.endpoint)
			}
			assert.Equal(t, tc.wantCalls, endpoints)
			require.NotNil(t, store.current)
			assert.Equal(t, "t1", store.current.TrackID, "the finished request must not be credited")
		})
	}
}
