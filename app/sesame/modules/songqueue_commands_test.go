// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The standalone !song / !skip spellings. Kept out of songqueue_test.go, which
// already carries the add, retract, remove and view behaviour of !sr itself.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSongCommandViewsTheQueue(t *testing.T) {
	store := &fakeSongQueue{}
	m := SongQueue(songDeps(store, srSearchGossip()))

	cmd := findCmd(t, m, "song")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), songCtx("42", "alice"), "", col.emit))

	assert.NotEmpty(t, chatText(t, col.out), "!song answers even with an empty queue")
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
