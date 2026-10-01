// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"errors"
	"math"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func patchGambleRoll(t *testing.T, roll func() (int64, error)) {
	t.Helper()
	original := engine.RollGamble
	engine.RollGamble = roll
	t.Cleanup(func() { engine.RollGamble = original })
}

func pinRoll(t *testing.T, roll int64) {
	t.Helper()
	patchGambleRoll(t, func() (int64, error) { return roll, nil })
}

func loyaltyView(enabled bool, config string) []projection.ModuleView {
	return []projection.ModuleView{{Name: engine.LoyaltyModuleName, IsEnabled: enabled, Configs: []byte(config)}}
}

func gambleDeps(fake *fakeLoyalty, cd engine.CooldownStore, view []projection.ModuleView) engine.Deps {
	d := engine.Deps{Loyalty: fake, Cooldown: cd, Log: zap.NewNop()}
	if view != nil {
		d.Proj = &fakeProj{modules: view}
	}
	return d
}

func wagerOf(login string, amount int64, won bool) engine.PointWager {
	return engine.PointWager{BroadcasterID: 100, ViewerID: 42, Login: login, Amount: amount, Won: won}
}

func TestGamble(t *testing.T) {
	const maxBet = `{"maxBet":9223372036854775807}`
	const crumbs = `{"loseMessage":"@{user} busted {amount} {points}, {balance} left","pointsName":"crumbs"}`
	cases := []struct {
		name     string
		roll     int64
		who      string
		config   string
		text     string
		fake     fakeLoyalty
		view     []projection.ModuleView
		silent   bool
		exact    string
		contains []string
		excludes []string
		wagers   []engine.PointWager
	}{
		{name: "a win credits the stake", roll: 23, who: "alice", text: "!gamble 300",
			contains: []string{"@alice", "rolled 23", "won 300", "1534"}, wagers: []engine.PointWager{wagerOf("alice", 300, true)}},
		{name: "TestGambleLossAppliesAtomicWager", roll: 87, who: "alice", text: "!gamble 300",
			contains: []string{"lost 300", "934"}, wagers: []engine.PointWager{wagerOf("alice", 300, false)}},
		{name: "all stakes the whole balance up to the cap", roll: 50, who: "bob", text: "!gamble all",
			contains: []string{"won 1000"}, wagers: []engine.PointWager{wagerOf("bob", 1000, true)}},
		{name: "a bet the balance cannot cover moves nothing", who: "bob", config: `{"maxBet":5000}`, text: "!gamble 2000",
			contains: []string{"can't cover that"}},
		{name: "no amount prints usage", who: "alice", config: `{"minBet":10,"maxBet":500}`, text: "!gamble", contains: []string{"!gamble"}},
		{name: "a bet under the minimum is refused", who: "alice", config: `{"minBet":10,"maxBet":500}`, text: "!gamble 5",
			contains: []string{"minimum bet is 10"}},
		{name: "a bet over the maximum is refused", who: "alice", config: `{"minBet":10,"maxBet":500}`, text: "!gamble 900",
			contains: []string{"max bet is 500"}},
		{name: "an unseen viewer is refused, not broke-shamed", roll: 50, who: "ghost", text: "!gamble 50",
			contains: []string{"haven't seen"}, wagers: []engine.PointWager{wagerOf("ghost", 50, true)}},
		{name: "custom templates fill the wager tokens", roll: 99, who: "erin", config: crumbs, text: "!gamble 50",
			exact: "@erin busted 50 crumbs, 1184 left", wagers: []engine.PointWager{wagerOf("erin", 50, false)}},
		{name: "loyalty's currency name wins over the leftover game blob", roll: 99, who: "erin", config: crumbs, text: "!gamble 50",
			view: loyaltyView(true, `{"pointsName":"bagels"}`), exact: "@erin busted 50 bagels, 1184 left", wagers: []engine.PointWager{wagerOf("erin", 50, false)}},
		{name: "stays silent while loyalty is off", who: "alice", text: "!gamble 300", view: loyaltyView(false, `{}`), silent: true},
		{name: "TestGambleRejectsBIGINTOverflowBeforeDebit doubled payout", who: "alice", config: maxBet, text: "!gamble 4611686018427387904",
			fake: fakeLoyalty{getPoints: math.MaxInt64/2 + 1}, contains: []string{"could exceed"}},
		{name: "TestGambleRejectsBIGINTOverflowBeforeDebit net winnings", who: "alice", config: maxBet, text: "!gamble 1",
			fake: fakeLoyalty{getPoints: math.MaxInt64}, contains: []string{"could exceed"}},
		{name: "TestGambleStaleCapacityRefusalDoesNotAnnounceWin", roll: 1, who: "alice", text: "!gamble 1",
			fake: fakeLoyalty{getPoints: math.MaxInt64 - 2, wagerLimit: true}, contains: []string{"could exceed"}, excludes: []string{"won"},
			wagers: []engine.PointWager{wagerOf("alice", 1, true)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pinRoll(t, tc.roll)
			fake := tc.fake
			cd := &fakeCooldown{}
			out := runChat(t, Gamble(gambleDeps(&fake, cd, tc.view)), withConfig(chatCtx("42", tc.who), tc.config), tc.text)
			if tc.silent {
				assert.Empty(t, out)
			} else {
				require.Len(t, out, 1)
				assertText(t, out[0].Text, textWant{tc.exact, tc.contains, tc.excludes})
			}
			assert.Equal(t, tc.wagers, fake.wagers)
			assert.Empty(t, fake.adjusts, "a wager is one atomic settlement, never the open-ended adjust")
		})
	}
}

func TestGambleRefusedWagersNeverClaimCooldown(t *testing.T) {
	cd := &fakeCooldown{}
	m := Gamble(gambleDeps(&fakeLoyalty{}, cd, nil))
	c := chatCtx("42", "alice")
	c.Config = []byte(`{"minBet":10,"maxBet":500}`)
	for _, text := range []string{"!gamble", "!gamble 5", "!gamble 900"} {
		runChat(t, m, c, text)
	}
	assert.Empty(t, cd.keys, "no refusal may burn the chatter's cooldown")
}

func TestGamblePerUserCooldown(t *testing.T) {
	pinRoll(t, 1)
	cd := &fakeCooldown{allow: []bool{true, false}}
	m := Gamble(gambleDeps(&fakeLoyalty{}, cd, nil))
	config := `{"cooldownSeconds":30}`

	carol := withConfig(chatCtx("42", "carol"), config)
	require.Len(t, runChat(t, m, carol, "!gamble 10"), 1)
	require.Len(t, cd.keys, 1)
	assert.Contains(t, cd.keys[0], "carol", "cooldown keys per user, not per channel")
	assert.Contains(t, cd.keys[0], "games:gamble:100")

	assert.Contains(t, runChat(t, m, carol, "!gamble 10")[0].Text, "breather", "a second wager inside the window is cooled")

	cd.allow = append(cd.allow, true)
	dave := withConfig(chatCtx("42", "dave"), config)
	assert.NotContains(t, runChat(t, m, dave, "!gamble 10")[0].Text, "breather")
}

func TestGambleRollFailureDoesNotMovePoints(t *testing.T) {
	patchGambleRoll(t, func() (int64, error) { return 0, errors.New("dice unavailable") })
	fake := &fakeLoyalty{}
	_, err := runChatErr(t, Gamble(gambleDeps(fake, nil, nil)), chatCtx("42", "alice"), "!gamble 100")
	require.Error(t, err)
	require.Empty(t, fake.wagers)
}
