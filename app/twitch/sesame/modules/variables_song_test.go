// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
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

type songVariableCase struct {
	name                     string
	reply                    gossiprpc.SpotifyNowPlayingReply
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
		{name: "transport failure skips queue", gossipErr: failure, current: queued, wantErr: failure},
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
		deps.Gossip = &fakeGossip{replies: map[string]any{"spotify.nowplaying": tc.reply}, err: tc.gossipErr}
	}
	values, err := songVariables(deps)(context.Background(), urchinCtx(""))
	assert.ErrorIs(t, err, tc.wantErr)
	assert.Equal(t, tc.wantTitle, values["title"])
	assert.Equal(t, tc.wantRequester, values["req"])
	assert.Equal(t, tc.wantReads, queue.reads)
}
