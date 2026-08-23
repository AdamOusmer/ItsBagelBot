// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// gamesCtx builds a chat Context for chatter `login` carrying an optional
// module config blob, the shape both wager games decode.
func gamesCtx(login, config string) *module.Context {
	c := &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			BroadcasterUserID: "100",
			ChatterUserID:     "42",
			ChatterUserLogin:  login,
		},
		BroadcasterID: 100,
		Log:           zap.NewNop(),
	}
	if config != "" {
		c.Config = []byte(config)
	}
	return c
}

// runGames runs a wager game's single command (each registers exactly one)
// against ctx and returns what it emitted.
func runGames(t *testing.T, m module.Module, c *module.Context, args string) []module.Output {
	t.Helper()
	var col collector
	require.NoError(t, m.Commands[0].Run(t.Context(), c, args, col.emit))
	return col.out
}

// pinRoll pins the gamble dice for the duration of one test.
func pinRoll(t *testing.T, roll int64) {
	t.Helper()
	old := engine.RollGamble
	engine.RollGamble = func() (int64, error) { return roll, nil }
	t.Cleanup(func() { engine.RollGamble = old })
}

func TestGambleWinCreditsStake(t *testing.T) {
	pinRoll(t, 23)
	fake := &fakeLoyalty{}
	cd := &fakeCooldown{}
	m := Gamble(engine.Deps{Loyalty: fake, Cooldown: cd, Log: zap.NewNop()})

	out := runGames(t, m, gamesCtx("alice", ""), "300")
	require.Len(t, out, 1)
	assert.Equal(t, outgress.TypeChat, out[0].Type)
	assert.Contains(t, out[0].Text, "@alice")
	assert.Contains(t, out[0].Text, "rolled 23")
	assert.Contains(t, out[0].Text, "won 300")
	assert.Contains(t, out[0].Text, "1534", "the reply carries the post-wager standing")

	// Escrow-first: the stake was taken, then the win paid it back doubled.
	require.Len(t, fake.spends, 1)
	assert.Equal(t, int64(300), fake.spends[0].amount)
	require.Len(t, fake.adjusts, 1)
	assert.Equal(t, int64(600), fake.adjusts[0].value, "a win returns the stake plus its match")
}

func TestGambleLossDebitsThroughSpend(t *testing.T) {
	pinRoll(t, 87) // misses the default 50
	fake := &fakeLoyalty{}
	m := Gamble(engine.Deps{Loyalty: fake, Log: zap.NewNop()})

	out := runGames(t, m, gamesCtx("alice", ""), "300")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "lost 300")
	assert.Contains(t, out[0].Text, "934")

	require.Len(t, fake.spends, 1)
	assert.Equal(t, int64(300), fake.spends[0].amount)
	assert.Empty(t, fake.adjusts, "a loss never rides the open-ended adjust")
}

func TestGambleRefusedWagersNeverClaimCooldown(t *testing.T) {
	fake := &fakeLoyalty{}
	cd := &fakeCooldown{}
	cfg := `{"minBet":10,"maxBet":500}`
	m := Gamble(engine.Deps{Loyalty: fake, Cooldown: cd, Log: zap.NewNop()})

	out := runGames(t, m, gamesCtx("alice", cfg), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "!gamble")

	out = runGames(t, m, gamesCtx("alice", cfg), "5")
	assert.Contains(t, out[0].Text, "minimum bet is 10")

	out = runGames(t, m, gamesCtx("alice", cfg), "900")
	assert.Contains(t, out[0].Text, "max bet is 500")

	assert.Empty(t, cd.keys, "no refusal may burn the chatter's cooldown")
	assert.Empty(t, fake.spends)
	assert.Empty(t, fake.adjusts)
}

func TestGambleDerivedAndOverBalance(t *testing.T) {
	pinRoll(t, 50) // boundary wins at the default chance
	fake := &fakeLoyalty{}
	m := Gamble(engine.Deps{Loyalty: fake, Log: zap.NewNop()})

	// The fake's standing is 1234; "all" caps at the default 1000 max.
	out := runGames(t, m, gamesCtx("bob", ""), "all")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "won 1000")
	require.Len(t, fake.adjusts, 1)
	assert.Equal(t, int64(2000), fake.adjusts[0].value, "the credit is stake plus match")

	// An explicit number under a raised cap but over the standing refuses on
	// funds rather than driving the balance negative.
	out = runGames(t, m, gamesCtx("bob", `{"maxBet":5000}`), "2000")
	assert.Contains(t, out[0].Text, "can't cover that")
	assert.Len(t, fake.adjusts, 1, "refusal moved nothing")
}

func TestGamblePerUserCooldown(t *testing.T) {
	pinRoll(t, 1)
	fake := &fakeLoyalty{}
	cd := &fakeCooldown{allow: []bool{true, false}}
	m := Gamble(engine.Deps{Loyalty: fake, Cooldown: cd, Log: zap.NewNop()})

	ctx := gamesCtx("carol", `{"cooldownSeconds":30}`)
	out := runGames(t, m, ctx, "10")
	require.Len(t, out, 1)
	require.Len(t, cd.keys, 1)
	assert.Contains(t, cd.keys[0], "carol", "cooldown keys per user, not per channel")
	assert.Contains(t, cd.keys[0], "games:gamble:100")

	out = runGames(t, m, ctx, "10")
	assert.Contains(t, out[0].Text, "breather", "second wager inside the window is cooled")

	// Another viewer is unaffected by carol's claim.
	cd.allow = append(cd.allow, true)
	out = runGames(t, m, gamesCtx("dave", `{"cooldownSeconds":30}`), "10")
	assert.NotContains(t, out[0].Text, "breather")
}

func TestGambleUnknownViewer(t *testing.T) {
	pinRoll(t, 50)
	fake := &fakeLoyalty{}
	m := Gamble(engine.Deps{Loyalty: fake, Log: zap.NewNop()})

	out := runGames(t, m, gamesCtx("ghost", ""), "50")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "haven't seen")
	assert.Empty(t, fake.adjusts, "an unseen viewer is refused, not broke-shamed")
}

func TestGambleCustomTemplates(t *testing.T) {
	pinRoll(t, 99)
	fake := &fakeLoyalty{spendBad: false}
	cfg := `{"loseMessage":"@{user} busted {amount} {points}, {balance} left","pointsName":"crumbs"}`
	m := Gamble(engine.Deps{Loyalty: fake, Log: zap.NewNop()})

	out := runGames(t, m, gamesCtx("erin", cfg), "50")
	require.Len(t, out, 1)
	assert.Equal(t, "@erin busted 50 crumbs, 1184 left", out[0].Text)
}
