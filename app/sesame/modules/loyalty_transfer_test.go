// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runPoints runs !points with the given args/config as a plain viewer
// (chatter 7 "coolviewer"). Returns the single reply line.
func runPoints(t *testing.T, fake *fakeLoyalty, config, args string) (string, *fakeLoyalty) {
	t.Helper()
	m := loyaltyModule(t, fake)
	cmd := loyaltyCommand(t, m, "points")
	var col collector
	require.NoError(t, cmd.Run(context.Background(), loyaltyCtx("channel.chat.message", "", config), args, col.emit))
	require.Len(t, col.out, 1)
	return col.out[0].Text, fake
}

func TestPointsGiveTransfersOwnPoints(t *testing.T) {
	fake := &fakeLoyalty{}
	text, _ := runPoints(t, fake, "", "give @bagelfan 500")

	require.Len(t, fake.transfers, 1)
	assert.Equal(t, transferCall{fromID: 7, login: "bagelfan", amount: 500}, fake.transfers[0])
	assert.Empty(t, fake.adjusts, "give must never take the mod-grant path")
	assert.Contains(t, text, "bagelfan")
	assert.Contains(t, text, "500")
}

func TestPointsGiveDisabledByConfig(t *testing.T) {
	fake := &fakeLoyalty{}
	text, _ := runPoints(t, fake, `{"viewerTransfers":-1}`, "give bagelfan 100")
	assert.Len(t, fake.transfers, 0)
	assert.Contains(t, strings.ToLower(text), "turned off")

	// A zero value keeps the default: transfers on.
	fake = &fakeLoyalty{}
	runPoints(t, fake, `{"viewerTransfers":0}`, "give bagelfan 100")
	assert.Len(t, fake.transfers, 1)
}

func TestPointsGiveGuards(t *testing.T) {
	// Self-transfer is refused before it reaches the ledger.
	fake := &fakeLoyalty{}
	text, _ := runPoints(t, fake, "", "give @CoolViewer 10")
	assert.Len(t, fake.transfers, 0)
	assert.Contains(t, strings.ToLower(text), "yourself")

	// Insufficient points reports what the sender actually holds.
	fake = &fakeLoyalty{transferBad: true}
	text, _ = runPoints(t, fake, "", "give bagelfan 999999")
	assert.Contains(t, text, "1234")

	// Unknown target.
	fake = &fakeLoyalty{}
	text, _ = runPoints(t, fake, "", "give ghost 10")
	assert.Contains(t, strings.ToLower(text), "haven't seen")

	// Bad amounts fall back to usage.
	fake = &fakeLoyalty{}
	text, _ = runPoints(t, fake, "", "give bagelfan nope")
	assert.Contains(t, strings.ToLower(text), "usage")
	assert.Len(t, fake.transfers, 0)
}

func TestPointsRemoveSubtracts(t *testing.T) {
	// The broadcaster removes a positive amount; the ledger sees a negative
	// delta adjust.
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	cmd := loyaltyCommand(t, m, "points")
	var col collector
	ctx := loyaltyCtx("channel.chat.message", "", "")
	ctx.Env.ChatterUserID = "2" // broadcaster shortcut
	require.NoError(t, cmd.Run(context.Background(), ctx, "remove @CoolViewer 100", col.emit))
	require.Len(t, fake.adjusts, 1)
	assert.Equal(t, int64(-100), fake.adjusts[0].value)
	assert.False(t, fake.adjusts[0].absolute)
	assert.Contains(t, col.out[0].Text, "1134") // 1234 - 100
}

func TestPointsGrantTogglesGateMods(t *testing.T) {
	// set switched off: a moderator's "!points set" gets the denial instead
	// of the grant — while add still works.
	fake := &fakeLoyalty{}
	m := loyaltyModule(t, fake)
	cmd := loyaltyCommand(t, m, "points")

	modRun := func(config, args string) string {
		var col collector
		ctx := loyaltyCtx("channel.chat.message", "", config)
		// A moderator who is not the channel owner: the badges carry the
		// role, and the owner shortcut (chatter == broadcaster) stays off.
		ctx.Env.ChatterUserID = "9"
		ctx.Env.Badges = []lane.Badge{{SetID: "moderator"}}
		require.NoError(t, cmd.Run(context.Background(), ctx, args, col.emit))
		require.Len(t, col.out, 1)
		return col.out[0].Text
	}

	cfg := `{"modSetPoints":-1}`
	assert.Contains(t, strings.ToLower(modRun(cfg, "set coolviewer 50")), "turned off")
	assert.Empty(t, fake.adjusts)

	assert.Contains(t, modRun(cfg, "add coolviewer 50"), "1284") // 1234 + 50
	require.Len(t, fake.adjusts, 1)
	assert.Equal(t, int64(50), fake.adjusts[0].value)

	// Both deltas switched off.
	cfg = `{"modAdjustPoints":-1}`
	assert.Contains(t, strings.ToLower(modRun(cfg, "add coolviewer 50")), "turned off")
	assert.Len(t, fake.adjusts, 1)

	// A non-mod typing remove still falls through to their own standing,
	// never the denial (the gate only speaks to moderators).
	var col collector
	ctx := loyaltyCtx("channel.chat.message", "", "")
	require.NoError(t, cmd.Run(context.Background(), ctx, "remove coolviewer 50", col.emit))
	assert.Len(t, fake.adjusts, 1, "non-mod must not reach the adjust path")
	assert.Contains(t, col.out[0].Text, "1234")

	// The broadcaster outranks every toggle: set stays available even with
	// modSetPoints switched off.
	fake = &fakeLoyalty{}
	m = loyaltyModule(t, fake)
	cmd = loyaltyCommand(t, m, "points")
	var owner collector
	octx := loyaltyCtx("channel.chat.message", "", `{"modSetPoints":-1}`)
	octx.Env.ChatterUserID = "2"
	require.NoError(t, cmd.Run(context.Background(), octx, "set coolviewer 50", owner.emit))
	require.Len(t, fake.adjusts, 1)
	assert.Equal(t, int64(50), fake.adjusts[0].value)
}

func TestLeaderboardShowsTopStandings(t *testing.T) {
	fake := &fakeLoyalty{topViewers: []topViewer{
		{id: "8", login: "alpha", name: "Alpha", points: 9000},
		{id: "9", login: "beta", name: "Beta", points: 800},
		{id: "10", login: "gamma", name: "", points: 7},
	}}
	m := loyaltyModule(t, fake)
	cmd := loyaltyCommand(t, m, "leaderboard")
	assert.Equal(t, "leaderboard", cmd.Name)

	var col collector
	require.NoError(t, cmd.Run(context.Background(), loyaltyCtx("channel.chat.message", "", ""), "2", col.emit))
	require.Len(t, col.out, 1)
	text := col.out[0].Text
	assert.Contains(t, text, "1. Alpha 9000")
	assert.Contains(t, text, "2. Beta 800")
	assert.NotContains(t, text, "gamma", "the limit caps the list")

	// Default size and empty board.
	fake = &fakeLoyalty{}
	m = loyaltyModule(t, fake)
	cmd = loyaltyCommand(t, m, "leaderboard")
	var empty collector
	require.NoError(t, cmd.Run(context.Background(), loyaltyCtx("channel.chat.message", "", ""), "", empty.emit))
	assert.Empty(t, fake.topViewers)
	require.Len(t, empty.out, 1)
	assert.Contains(t, strings.ToLower(empty.out[0].Text), "no standings")

	// A silly argument answers with usage, not a service call.
	fake = &fakeLoyalty{topViewers: []topViewer{{id: "8", name: "A", points: 1}}}
	m = loyaltyModule(t, fake)
	cmd = loyaltyCommand(t, m, "leaderboard")
	var bad collector
	require.NoError(t, cmd.Run(context.Background(), loyaltyCtx("channel.chat.message", "", ""), "99", bad.emit))
	require.Len(t, bad.out, 1)
	assert.Contains(t, strings.ToLower(bad.out[0].Text), "usage")
}
