// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeDuel is an in-memory DuelStore for the module tests: every method
// records its request and replays a scripted outcome, so the router's reply
// mapping is pinned without any Valkey or wallet underneath.
type fakeDuel struct {
	openSpec engine.DuelOpenSpec // last Open request
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
}

func (f *fakeDuel) Open(_ context.Context, _ uint64, spec engine.DuelOpenSpec) (engine.DuelOpenResult, error) {
	f.openSpec = spec
	return f.openRes, f.openErr
}

func (f *fakeDuel) Join(_ context.Context, _ uint64, login string, stake int64) (engine.DuelJoinResult, error) {
	f.joinLogin, f.joinStake = login, stake
	return f.joinRes, nil
}

func (f *fakeDuel) Accept(_ context.Context, _ uint64, login string) (engine.DuelAcceptResult, error) {
	f.acceptLogin = login
	return f.acceptRes, nil
}

func (f *fakeDuel) Decline(_ context.Context, _ uint64, login string) (engine.DuelDeclineResult, error) {
	f.declineLogin = login
	return f.declineRes, nil
}

func (f *fakeDuel) Cancel(_ context.Context, _ uint64, byLogin string, moderator bool) (engine.DuelCancelResult, error) {
	f.cancelLogin, f.cancelMod, f.cancelCalled = byLogin, moderator, true
	return f.cancelRes, nil
}

func (f *fakeDuel) Status(_ context.Context, _ uint64) (engine.DuelStatus, error) {
	return f.statusRes, nil
}

func (f *fakeDuel) StartExpiryWatcher(context.Context) {}

func duelDeps(f *fakeDuel) engine.Deps {
	return engine.Deps{Duel: f, Log: zap.NewNop()}
}

// --- status ---

func TestDuelStatusNone(t *testing.T) {
	m := Duel(duelDeps(&fakeDuel{}))
	out := runGames(t, m, gamesCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "No duel running")
}

func TestDuelStatusPot(t *testing.T) {
	f := &fakeDuel{statusRes: engine.DuelStatus{
		Open: true, Kind: engine.DuelPot, Opener: "opener",
		Pot: 700, Entrants: 4, Stake: 100, SecondsLeft: 42,
	}}
	m := Duel(duelDeps(f))
	out := runGames(t, m, gamesCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "4 in")
	assert.Contains(t, out[0].Text, "700 points in the pot")
	assert.Contains(t, out[0].Text, "~42s")
}

func TestDuelStatusChallenge(t *testing.T) {
	f := &fakeDuel{statusRes: engine.DuelStatus{
		Open: true, Kind: engine.DuelChallenge, Opener: "maya",
		Challenged: "crust", Stake: 500, SecondsLeft: 30,
	}}
	m := Duel(duelDeps(f))
	out := runGames(t, m, gamesCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@maya vs @crust")
	assert.Contains(t, out[0].Text, "500 points each")
}

// --- stake: open-or-join ---

func TestDuelJoinRunningPot(t *testing.T) {
	f := &fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Joined: true, Entrants: 3, Pot: 450}}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("bob", ""), "150")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@bob you're in with 150")
	assert.Contains(t, out[0].Text, "3 in the duel")
	assert.Contains(t, out[0].Text, "450 points")
	assert.Equal(t, "bob", f.joinLogin)
	assert.Equal(t, int64(150), f.joinStake)
	assert.Equal(t, engine.DuelOpenSpec{}, f.openSpec, "a join must not reach Open")
}

func TestDuelStakeOpensWhenIdle(t *testing.T) {
	f := &fakeDuel{openRes: engine.DuelOpenResult{Started: true}}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("bob", ""), "250")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "Pot duel is LIVE")
	assert.Contains(t, out[0].Text, "250 points")

	assert.Equal(t, engine.DuelPot, f.openSpec.Kind)
	assert.Equal(t, "bob", f.openSpec.Opener)
	assert.Equal(t, int64(250), f.openSpec.Stake)
	assert.Empty(t, f.openSpec.Challenged)
}

func TestDuelStakeLimitsAndUsage(t *testing.T) {
	f := &fakeDuel{}
	m := Duel(duelDeps(f))
	ctx := gamesCtx("bob", `{"minStake":10,"maxStake":900}`)

	out := runGames(t, m, ctx, "5")
	assert.Contains(t, out[0].Text, "minimum stake is 10")

	out = runGames(t, m, ctx, "1000")
	assert.Contains(t, out[0].Text, "max stake is 900")

	out = runGames(t, m, ctx, "lots")
	assert.Contains(t, out[0].Text, "!duel <amount>")

	assert.Empty(t, f.joinLogin, "refused stakes never reach the store")
}

func TestDuelJoinBlockedByChallenge(t *testing.T) {
	f := &fakeDuel{joinRes: engine.DuelJoinResult{Open: true, ChallengePending: true}}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("bob", ""), "100")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "head-to-head challenge is pending")
}

func TestDuelJoinRefusals(t *testing.T) {
	cases := []struct {
		name string
		res  engine.DuelJoinResult
		want string
	}{
		{"short", engine.DuelJoinResult{Open: true, Short: true}, "don't have enough"},
		{"unknown", engine.DuelJoinResult{Open: true, Unknown: true}, "haven't seen"},
		{"already", engine.DuelJoinResult{Open: true, Already: true, Entrants: 2, Pot: 300}, "already in this duel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Duel(duelDeps(&fakeDuel{joinRes: tc.res}))
			out := runGames(t, m, gamesCtx("bob", ""), "100")
			require.Len(t, out, 1)
			assert.Contains(t, out[0].Text, tc.want)
		})
	}
}

// --- challenge ---

func TestDuelChallengeSent(t *testing.T) {
	f := &fakeDuel{openRes: engine.DuelOpenResult{Started: true}}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("maya", ""), "@crust 400")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@maya challenges @crust for 400 points")
	assert.Contains(t, out[0].Text, "winner takes 800", "the reply names the doubled pot")

	assert.Equal(t, engine.DuelChallenge, f.openSpec.Kind)
	assert.Equal(t, "maya", f.openSpec.Opener)
	assert.Equal(t, "crust", f.openSpec.Challenged, "the @ prefix is stripped before the store sees it")
	assert.Equal(t, int64(400), f.openSpec.Stake)
}

func TestDuelChallengeEmptyTarget(t *testing.T) {
	f := &fakeDuel{}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("maya", ""), "@ 400")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "!duel <amount>")
	assert.Empty(t, f.openSpec.Kind, "a targetless challenge never reaches the store")
}

func TestDuelAcceptUnpaid(t *testing.T) {
	f := &fakeDuel{acceptRes: engine.DuelAcceptResult{
		Found: true, Accepted: true, Unpaid: true, Winner: "crust", Loser: "maya", Pot: 800,
	}}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("crust", ""), "accept")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@crust takes the 800 points")
	assert.Contains(t, out[0].Text, "payout is landing")
}

func TestDuelChallengeSelf(t *testing.T) {
	f := &fakeDuel{}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("maya", ""), "maya 400")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "can't duel yourself")
	assert.Empty(t, f.openSpec.Kind, "a self-duel never reaches the store")
}

// --- accept ---

func TestDuelAcceptOutcomes(t *testing.T) {
	cases := []struct {
		name string
		res  engine.DuelAcceptResult
		want []string
	}{
		{
			name: "clean win",
			res:  engine.DuelAcceptResult{Found: true, Accepted: true, Winner: "crust", Loser: "maya", Pot: 800, Stake: 400},
			want: []string{"@crust defeats @maya", "takes 800 points"},
		},
		{
			name: "unpaid payout",
			res:  engine.DuelAcceptResult{Found: true, Accepted: true, Unpaid: true, Winner: "crust", Loser: "maya", Pot: 800},
			want: []string{"@crust takes the 800 points", "payout is landing"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDuel{acceptRes: tc.res}
			m := Duel(duelDeps(f))
			out := runGames(t, m, gamesCtx("crust", ""), "accept")
			require.Len(t, out, 1)
			assert.Equal(t, "crust", f.acceptLogin)
			for _, want := range tc.want {
				assert.Contains(t, out[0].Text, want)
			}
		})
	}
}

func TestDuelAcceptRefusals(t *testing.T) {
	cases := []struct {
		name string
		res  engine.DuelAcceptResult
		want string
	}{
		{"none", engine.DuelAcceptResult{}, "no challenge is waiting"},
		{"notYou", engine.DuelAcceptResult{Found: true, WrongUser: true}, "only the challenged party"},
		{"short", engine.DuelAcceptResult{Found: true, Short: true}, "can't cover the stake"},
		{"unknown", engine.DuelAcceptResult{Found: true, Unknown: true}, "haven't seen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Duel(duelDeps(&fakeDuel{acceptRes: tc.res}))
			out := runGames(t, m, gamesCtx("crust", ""), "accept")
			require.Len(t, out, 1)
			assert.Contains(t, out[0].Text, tc.want)
		})
	}
}

// --- decline / cancel ---

func TestDuelDeclineRefundsOpener(t *testing.T) {
	f := &fakeDuel{declineRes: engine.DuelDeclineResult{
		Found: true, Declined: true, Opener: "maya", Refund: 400,
	}}
	m := Duel(duelDeps(f))

	out := runGames(t, m, gamesCtx("crust", ""), "decline")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@crust declined the challenge")
	assert.Contains(t, out[0].Text, "@maya's 400 points are back")
	assert.Equal(t, "crust", f.declineLogin)
}

func TestDuelCancelAuthorization(t *testing.T) {
	t.Run("denied viewer", func(t *testing.T) {
		f := &fakeDuel{cancelRes: engine.DuelCancelResult{Found: true}}
		m := Duel(duelDeps(f))
		out := runGames(t, m, queueCtx("randy", ""), "cancel")
		require.Len(t, out, 1)
		assert.Contains(t, out[0].Text, "only the opener or a moderator")
	})
	t.Run("moderator passes the flag through", func(t *testing.T) {
		f := &fakeDuel{cancelRes: engine.DuelCancelResult{
			Cancelled: true, Refunded: 3, Total: 1500,
		}}
		m := Duel(duelDeps(f))
		out := runGames(t, m, queueCtx("mod_kim", "moderator"), "cancel")
		require.Len(t, out, 1)
		assert.True(t, f.cancelMod, "the chatter's moderator role rides to the store")
		assert.Contains(t, out[0].Text, "Duel cancelled — 3 refunded, 1500 points returned")
	})
	t.Run("nothing running", func(t *testing.T) {
		f := &fakeDuel{}
		m := Duel(duelDeps(f))
		out := runGames(t, m, queueCtx("mod_kim", "moderator"), "cancel")
		require.Len(t, out, 1)
		assert.Contains(t, out[0].Text, "No duel running")
		assert.True(t, f.cancelCalled, "the module asks the store; the store's not-found drives the reply")
	})
}

// --- inert without a store ---

func TestGambleAndDuelInertWithoutStores(t *testing.T) {
	var col collector
	gm := Gamble(engine.Deps{Log: zap.NewNop()})
	dm := Duel(engine.Deps{Log: zap.NewNop()})
	require.NoError(t, findCmd(t, gm, "gamble").Run(context.Background(), gamesCtx("x", ""), "all", col.emit))
	require.NoError(t, findCmd(t, dm, "duel").Run(context.Background(), gamesCtx("x", ""), "", col.emit))
	assert.Empty(t, col.out, "nil stores leave both modules silent, not panicking")
}

func TestDuelInertWhenLoyaltyOff(t *testing.T) {
	m := Duel(engine.Deps{Duel: &fakeDuel{}, Proj: loyaltyProj{on: false}, Log: zap.NewNop()})
	out := runGames(t, m, gamesCtx("alice", ""), "")
	assert.Empty(t, out, "an enabled duel row still stays silent while loyalty is off")
}
