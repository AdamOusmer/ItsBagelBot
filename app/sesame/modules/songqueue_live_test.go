// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The live-only gate on song requests, and the standalone !song / !skip
// spellings. Kept out of songqueue_test.go, which already carries the add,
// retract, remove and view behaviour.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// songLiveDeps is songDeps plus a live store, which the gate consults.
func songLiveDeps(store engine.SongQueueStore, g engine.GossipCaller, live engine.LiveStore) engine.Deps {
	return engine.Deps{SongQueue: store, Gossip: g, Live: live, Log: zap.NewNop()}
}

func songCtxCfg(config string, chatterID, login string, badges ...string) *module.Context {
	c := songCtx(chatterID, login, badges...)
	c.Config = []byte(config)
	return c
}

func TestSRRefusesAddWhileOffline(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "Human", "The Killers"))
	store := &fakeSongQueue{}
	m := SongQueue(songLiveDeps(store, g, &fakeLive{live: false}))

	out := runSR(t, m, songCtx("42", "alice"), "human")

	assert.Empty(t, store.up, "an offline stream must not accept requests")
	assert.Contains(t, chatText(t, out), "live")
}

func TestSRAcceptsAddWhileLive(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "Human", "The Killers"))
	store := &fakeSongQueue{}
	m := SongQueue(songLiveDeps(store, g, &fakeLive{live: true}))

	runSR(t, m, songCtx("42", "alice"), "human")

	require.Len(t, store.up, 1)
}

// The flag is stored as its inverse so the dashboard can present "Live only"
// as an on-by-default switch, exactly like govee's.
func TestSRAllowOfflineOptsOutOfTheGate(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "Human", "The Killers"))
	store := &fakeSongQueue{}
	m := SongQueue(songLiveDeps(store, g, &fakeLive{live: false}))

	runSR(t, m, songCtxCfg(`{"allowOffline":true}`, "42", "alice"), "human")

	require.Len(t, store.up, 1, "allowOffline lifts the live-only gate")
}

// Fail-closed: the gate exists to keep the queue empty while the broadcaster is
// away, so a live check that errors must refuse rather than assume.
func TestSRLiveCheckFailureRefusesTheAdd(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "Human", "The Killers"))
	store := &fakeSongQueue{}
	m := SongQueue(songLiveDeps(store, g, &fakeLive{err: errors.New("valkey down")}))

	runSR(t, m, songCtx("42", "alice"), "human")

	assert.Empty(t, store.up, "a failed live check must not open the queue")
}

// A deployment with no live store wired must not lose the feature outright.
func TestSRWithoutLiveStoreStillAccepts(t *testing.T) {
	g := srSearchGossip(srTrack("t1", "Human", "The Killers"))
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, g))

	runSR(t, m, songCtx("42", "alice"), "human")

	require.Len(t, store.up, 1)
}

// Reading the queue is not gated: a broadcaster tidies up before going live.
func TestSongCommandViewsWhileOffline(t *testing.T) {
	store := &fakeSongQueue{}
	m := SongQueue(songLiveDeps(store, srSearchGossip(), &fakeLive{live: false}))

	cmd := findCmd(t, m, "song")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), songCtx("42", "alice"), "", col.emit))

	assert.NotEmpty(t, chatText(t, col.out), "!song answers even offline")
}

func TestSongCommandCarriesCurrentAlias(t *testing.T) {
	m := SongQueue(songDeps(&fakeSongQueue{}, srSearchGossip()))
	cmd := findCmd(t, m, "song")
	assert.Contains(t, cmd.Aliases, "current")
	assert.Contains(t, cmd.Aliases, "nowplaying")
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
