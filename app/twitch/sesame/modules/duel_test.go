// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
)

type fakeDuel struct {
	openSpec engine.DuelOpenSpec
	openRes  engine.DuelOpenResult
	openErr  error

	joinLogin string
	joinStake int64
	joinRes   engine.DuelJoinResult

	acceptLogin string
	acceptRes   engine.DuelAcceptResult

	declineLogin string
	declineRes   engine.DuelDeclineResult

	cancelLogin  string
	cancelMod    bool
	cancelCalled bool
	cancelRes    engine.DuelCancelResult

	statusRes engine.DuelStatus
	callerIDs []uint64
}

func (f *fakeDuel) Open(_ context.Context, _ uint64, spec engine.DuelOpenSpec) (engine.DuelOpenResult, error) {
	f.openSpec = spec
	return f.openRes, f.openErr
}

func (f *fakeDuel) Join(_ context.Context, _ uint64, who engine.DuelParty, stake int64) (engine.DuelJoinResult, error) {
	f.joinLogin, f.joinStake = who.Login, stake
	f.callerIDs = append(f.callerIDs, who.ID)
	return f.joinRes, nil
}

func (f *fakeDuel) Accept(_ context.Context, _ uint64, who engine.DuelParty) (engine.DuelAcceptResult, error) {
	f.acceptLogin = who.Login
	f.callerIDs = append(f.callerIDs, who.ID)
	return f.acceptRes, nil
}

func (f *fakeDuel) Decline(_ context.Context, _ uint64, who engine.DuelParty) (engine.DuelDeclineResult, error) {
	f.declineLogin = who.Login
	f.callerIDs = append(f.callerIDs, who.ID)
	return f.declineRes, nil
}

func (f *fakeDuel) Cancel(_ context.Context, _ uint64, who engine.DuelParty, moderator bool) (engine.DuelCancelResult, error) {
	f.cancelLogin, f.cancelMod, f.cancelCalled = who.Login, moderator, true
	f.callerIDs = append(f.callerIDs, who.ID)
	return f.cancelRes, nil
}

func (f *fakeDuel) Status(_ context.Context, _ uint64) (engine.DuelStatus, error) {
	return f.statusRes, nil
}

func (f *fakeDuel) StartExpiryWatcher(context.Context) {}

func duelDeps(f *fakeDuel) engine.Deps {
	return engine.Deps{Duel: f, Loyalty: &fakeLoyalty{}, Log: zap.NewNop()}
}

type duelCalls struct {
	open         engine.DuelOpenSpec
	joinLogin    string
	joinStake    int64
	acceptLogin  string
	declineLogin string
	cancelLogin  string
	cancelMod    bool
	cancelCalled bool
}

func (f *fakeDuel) calls() duelCalls {
	return duelCalls{f.openSpec, f.joinLogin, f.joinStake, f.acceptLogin, f.declineLogin, f.cancelLogin, f.cancelMod, f.cancelCalled}
}

var (
	potStatus       = engine.DuelStatus{Open: true, Kind: engine.DuelPot, Opener: "opener", Pot: 700, Entrants: 4, Stake: 100, SecondsLeft: 42}
	challengeStatus = engine.DuelStatus{Open: true, Kind: engine.DuelChallenge, Opener: "maya", Challenged: "crust", Stake: 500, SecondsLeft: 30}
	unpaidAccept    = engine.DuelAcceptResult{Found: true, Accepted: true, Unpaid: true, Winner: "crust", Loser: "maya", Pot: 800}
)

type duelCase struct {
	name     string
	who      string
	mod      bool
	text     string
	config   string
	balances map[uint64]int64
	fake     fakeDuel
	contains []string
	calls    duelCalls
}

var duelCases = []duelCase{
	{name: "status reports no duel running", who: "alice", text: "!duel", contains: []string{"No duel running"}},
	{name: "status describes a running pot", who: "alice", text: "!duel", fake: fakeDuel{statusRes: potStatus},
		contains: []string{"4 in", "700 points in the pot", "~42s"}},
	{name: "status describes a pending challenge", who: "alice", text: "!duel", fake: fakeDuel{statusRes: challengeStatus},
		contains: []string{"@maya vs @crust", "500 points each"}},
	{name: "a stake joins the running pot", who: "bob", text: "!duel 150",
		fake:     fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Joined: true, Entrants: 3, Pot: 450}},
		contains: []string{"@bob you're in with 150", "3 in the duel", "450 points"}, calls: duelCalls{joinLogin: "bob", joinStake: 150}},
	{name: "a stake opens a pot duel when idle", who: "bob", text: "!duel 250", fake: fakeDuel{openRes: engine.DuelOpenResult{Started: true}},
		contains: []string{"Pot duel is LIVE", "250 points"},
		calls:    duelCalls{joinLogin: "bob", joinStake: 250, open: engine.DuelOpenSpec{Kind: engine.DuelPot, Opener: engine.DuelParty{ID: 42, Login: "bob"}, Stake: 250}}},
	{name: "a stake under the minimum is refused", who: "bob", text: "!duel 5", config: `{"minStake":10,"maxStake":900}`,
		contains: []string{"minimum stake is 10"}},
	{name: "a stake over the maximum is refused", who: "bob", text: "!duel 1000", config: `{"minStake":10,"maxStake":900}`,
		contains: []string{"max stake is 900"}},
	{name: "a non numeric stake prints usage", who: "bob", text: "!duel lots", config: `{"minStake":10,"maxStake":900}`,
		contains: []string{"!duel <amount|half|all>"}},
	{name: "stake limits saved as text by the dashboard are honored", who: "bob", text: "!duel 1000", config: `{"minStake":"10","maxStake":"900"}`,
		contains: []string{"max stake is 900"}},
	{name: "a pot window saved as text sets the window", who: "bob", text: "!duel 250", config: `{"potSeconds":"90"}`,
		fake:     fakeDuel{openRes: engine.DuelOpenResult{Started: true}},
		contains: []string{"Pot duel is LIVE", "90s"},
		calls:    duelCalls{joinLogin: "bob", joinStake: 250, open: engine.DuelOpenSpec{Kind: engine.DuelPot, Opener: engine.DuelParty{ID: 42, Login: "bob"}, Stake: 250, PotSeconds: 90}}},
	{name: "a stake is blocked while a challenge is pending", who: "bob", text: "!duel 100",
		fake:     fakeDuel{joinRes: engine.DuelJoinResult{Open: true, ChallengePending: true}},
		contains: []string{"head-to-head challenge is pending"}, calls: duelCalls{joinLogin: "bob", joinStake: 100}},
	{name: "a short wallet cannot join", who: "bob", text: "!duel 100", fake: fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Short: true}},
		contains: []string{"don't have enough"}, calls: duelCalls{joinLogin: "bob", joinStake: 100}},
	{name: "an unseen viewer cannot join", who: "bob", text: "!duel 100", fake: fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Unknown: true}},
		contains: []string{"haven't seen"}, calls: duelCalls{joinLogin: "bob", joinStake: 100}},
	{name: "joining twice is refused", who: "bob", text: "!duel 100",
		fake:     fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Already: true, Entrants: 2, Pot: 300}},
		contains: []string{"already in this duel"}, calls: duelCalls{joinLogin: "bob", joinStake: 100}},
	{name: "a challenge names the doubled pot", who: "maya", text: "!duel @crust 400", fake: fakeDuel{openRes: engine.DuelOpenResult{Started: true}},
		contains: []string{"@maya challenges @crust for 400 points", "Winner takes 800"},
		calls:    duelCalls{open: engine.DuelOpenSpec{Kind: engine.DuelChallenge, Opener: engine.DuelParty{ID: 42, Login: "maya"}, Challenged: engine.DuelParty{ID: 8, Login: "crust"}, Stake: 400}}},
	{name: "all opens a pot with the whole balance", who: "bob", text: "!duel all", balances: map[uint64]int64{42: 300},
		fake:     fakeDuel{openRes: engine.DuelOpenResult{Started: true}},
		contains: []string{"Pot duel is LIVE", "300 points"},
		calls:    duelCalls{joinLogin: "bob", joinStake: 300, open: engine.DuelOpenSpec{Kind: engine.DuelPot, Opener: engine.DuelParty{ID: 42, Login: "bob"}, Stake: 300}}},
	{name: "half joins with half the balance", who: "bob", text: "!duel half", balances: map[uint64]int64{42: 301},
		fake:     fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Joined: true, Entrants: 2, Pot: 250}},
		contains: []string{"@bob you're in with 150"}, calls: duelCalls{joinLogin: "bob", joinStake: 150}},
	{name: "all caps at the max stake", who: "bob", text: "!duel all", config: `{"maxStake":900}`, balances: map[uint64]int64{42: 5000},
		fake:     fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Joined: true, Entrants: 2, Pot: 1800}},
		contains: []string{"@bob you're in with 900"}, calls: duelCalls{joinLogin: "bob", joinStake: 900}},
	{name: "a stake over the balance names the balance", who: "bob", text: "!duel 500", balances: map[uint64]int64{42: 120},
		contains: []string{"can't cover that stake, you have 120 points"}},
	{name: "all with an empty wallet names the balance", who: "bob", text: "!duel All", balances: map[uint64]int64{42: 0},
		contains: []string{"can't cover that stake, you have 0 points"}},
	{name: "half under the minimum quotes the minimum", who: "bob", text: "!duel half", config: `{"minStake":10}`, balances: map[uint64]int64{42: 15},
		contains: []string{"minimum stake is 10"}},
	{name: "a challenge with all stakes the whole balance", who: "maya", text: "!duel @crust all", balances: map[uint64]int64{42: 600, 8: 600},
		fake:     fakeDuel{openRes: engine.DuelOpenResult{Started: true}},
		contains: []string{"@maya challenges @crust for 600 points", "Winner takes 1200"},
		calls:    duelCalls{open: engine.DuelOpenSpec{Kind: engine.DuelChallenge, Opener: engine.DuelParty{ID: 42, Login: "maya"}, Challenged: engine.DuelParty{ID: 8, Login: "crust"}, Stake: 600}}},
	{name: "a challenge the target cannot cover is refused before escrow", who: "maya", text: "!duel @crust all", balances: map[uint64]int64{42: 600, 8: 200},
		contains: []string{"@maya @crust can't cover 600 points right now"}},
	{name: "all from the poorer challenger stakes only the challenger's balance", who: "maya", text: "!duel @crust all", config: `{"maxStake":5000}`,
		balances: map[uint64]int64{42: 1000, 8: 3000},
		fake:     fakeDuel{openRes: engine.DuelOpenResult{Started: true}},
		contains: []string{"@maya challenges @crust for 1000 points", "Winner takes 2000"},
		calls:    duelCalls{open: engine.DuelOpenSpec{Kind: engine.DuelChallenge, Opener: engine.DuelParty{ID: 42, Login: "maya"}, Challenged: engine.DuelParty{ID: 8, Login: "crust"}, Stake: 1000}}},
	{name: "all from the richer challenger is refused when the target cannot match", who: "maya", text: "!duel @crust all", config: `{"maxStake":5000}`,
		balances: map[uint64]int64{42: 3000, 8: 1000},
		contains: []string{"@maya @crust can't cover 3000 points right now"}},
	{name: "TestDuelChallengeEmptyTarget", who: "maya", text: "!duel @ 400", contains: []string{"!duel <amount|half|all>"}},
	{name: "dueling yourself is refused", who: "maya", text: "!duel maya 400", contains: []string{"can't duel yourself"}},
	{name: "accepting a clean win names the winner", who: "crust", text: "!duel accept",
		fake:     fakeDuel{acceptRes: engine.DuelAcceptResult{Found: true, Accepted: true, Winner: "crust", Loser: "maya", Pot: 800, Stake: 400}},
		contains: []string{"@crust defeats @maya", "takes 800 points"}, calls: duelCalls{acceptLogin: "crust"}},
	{name: "TestDuelAcceptUnpaid", who: "crust", text: "!duel accept", fake: fakeDuel{acceptRes: unpaidAccept},
		contains: []string{"@crust takes the 800 points", "Payout is landing"}, calls: duelCalls{acceptLogin: "crust"}},
	{name: "accepting with no challenge waiting", who: "crust", text: "!duel accept", contains: []string{"no challenge is waiting"}, calls: duelCalls{acceptLogin: "crust"}},
	{name: "only the challenged party may accept", who: "crust", text: "!duel accept", fake: fakeDuel{acceptRes: engine.DuelAcceptResult{Found: true, WrongUser: true}},
		contains: []string{"only the challenged party"}, calls: duelCalls{acceptLogin: "crust"}},
	{name: "a short wallet cannot cover the accepted stake", who: "crust", text: "!duel accept", fake: fakeDuel{acceptRes: engine.DuelAcceptResult{Found: true, Short: true}},
		contains: []string{"can't cover the stake"}, calls: duelCalls{acceptLogin: "crust"}},
	{name: "an unseen viewer cannot accept", who: "crust", text: "!duel accept", fake: fakeDuel{acceptRes: engine.DuelAcceptResult{Found: true, Unknown: true}},
		contains: []string{"haven't seen"}, calls: duelCalls{acceptLogin: "crust"}},
	{name: "declining refunds the opener", who: "crust", text: "!duel decline",
		fake:     fakeDuel{declineRes: engine.DuelDeclineResult{Found: true, Declined: true, Opener: "maya", Refund: 400}},
		contains: []string{"@crust declined the challenge", "@maya's 400 points are back"}, calls: duelCalls{declineLogin: "crust"}},
	{name: "a plain viewer cannot cancel", who: "randy", text: "!duel cancel", fake: fakeDuel{cancelRes: engine.DuelCancelResult{Found: true}},
		contains: []string{"only the opener or a moderator"}, calls: duelCalls{cancelLogin: "randy", cancelCalled: true}},
	{name: "a moderator cancel carries the role to the store", who: "mod_kim", mod: true, text: "!duel cancel",
		fake:     fakeDuel{cancelRes: engine.DuelCancelResult{Cancelled: true, Refunded: 3, Total: 1500}},
		contains: []string{"Duel cancelled", "3 refunded, 1500 points returned"}, calls: duelCalls{cancelLogin: "mod_kim", cancelMod: true, cancelCalled: true}},
	{name: "cancelling with nothing running reports it", who: "mod_kim", mod: true, text: "!duel cancel",
		contains: []string{"No duel running"}, calls: duelCalls{cancelLogin: "mod_kim", cancelMod: true, cancelCalled: true}},
}

func TestDuelChat(t *testing.T) {
	cases := duelCases
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := tc.fake
			c := chatCtx("42", tc.who)
			if tc.mod {
				c = chatCtx("42", tc.who, "moderator")
			}
			d := duelDeps(&fake)
			d.Loyalty = &fakeLoyalty{viewerPts: tc.balances}
			out := runChat(t, Duel(d), withConfig(c, tc.config), tc.text)
			require.Len(t, out, 1)
			for _, want := range tc.contains {
				assert.Contains(t, out[0].Text, want)
			}
			assert.Equal(t, tc.calls, fake.calls())
		})
	}
}

func TestGamesStaySilentWhenUnavailable(t *testing.T) {
	cases := []struct {
		name string
		m    module.Module
		text string
	}{
		{"gamble without a loyalty store", Gamble(engine.Deps{Log: zap.NewNop()}), "!gamble all"},
		{"duel without a duel store", Duel(engine.Deps{Log: zap.NewNop()}), "!duel"},
		{"duel while loyalty is off", Duel(gamblelessDuelDeps()), "!duel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, runChat(t, tc.m, chatCtx("42", "x"), tc.text))
		})
	}
}

func gamblelessDuelDeps() engine.Deps {
	d := duelDeps(&fakeDuel{})
	d.Proj = &fakeProj{modules: loyaltyView(false, `{}`)}
	return d
}
