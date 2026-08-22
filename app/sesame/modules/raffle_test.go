// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/internal/domain/outgress"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeRaffle is an in-memory RaffleStore for the module tests: a slice for the
// entrant pool, bools for the open flag and last receipt, a scripted draw
// result the Draw path hands back verbatim, and a scripted claim outcome.
type fakeRaffle struct {
	open        bool
	pool        []string
	lastSpec    engine.RaffleOpenSpec // last Open's request
	drawResult  *engine.RaffleResult // nil: Draw reports "nothing running"
	lastResult  *engine.RaffleResult
	lastFound   bool
	claimScript func(login string) engine.RaffleClaim
	err         error
}

func (f *fakeRaffle) Open(_ context.Context, _ uint64, spec engine.RaffleOpenSpec) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.open {
		return false, nil
	}
	f.open = true
	f.lastSpec = spec
	return true, nil
}

func (f *fakeRaffle) Join(_ context.Context, _ uint64, userID string) (bool, int64, bool, error) {
	if f.err != nil {
		return false, 0, false, f.err
	}
	for _, p := range f.pool {
		if p == userID {
			return false, int64(len(f.pool)), f.open, nil
		}
	}
	if !f.open {
		return false, int64(len(f.pool)), false, nil
	}
	f.pool = append(f.pool, userID)
	return true, int64(len(f.pool)), true, nil
}

func (f *fakeRaffle) Status(_ context.Context, _ uint64) (bool, int64, int64, error) {
	return f.open, int64(len(f.pool)), 600, f.err
}

func (f *fakeRaffle) Draw(_ context.Context, _ uint64, _ int64) (*engine.RaffleResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.open = false
	return f.drawResult, nil
}

func (f *fakeRaffle) Cancel(_ context.Context, _ uint64) (bool, error) {
	was := f.open
	f.open = false
	return was, f.err
}

func (f *fakeRaffle) LastResult(_ context.Context, _ uint64) (*engine.RaffleResult, bool, error) {
	if !f.lastFound {
		return nil, false, f.err
	}
	return f.lastResult, true, f.err
}

func (f *fakeRaffle) Claim(_ context.Context, _ uint64, login string) (engine.RaffleClaim, error) {
	if f.claimScript == nil {
		return engine.ClaimNone, nil
	}
	return f.claimScript(login), nil
}

func (f *fakeRaffle) StartExpiryWatcher(context.Context) {}

func raffleDeps(r engine.RaffleStore) engine.Deps {
	return engine.Deps{Raffle: r, Log: zap.NewNop()}
}

// --- join ---

func TestRaffleJoinOpen(t *testing.T) {
	r := &fakeRaffle{open: true}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "join", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Equal(t, outgress.TypeChat, out[0].Type)
	assert.Contains(t, out[0].Text, "@alice")
	assert.Contains(t, out[0].Text, "1 entered")
	assert.Equal(t, []string{"alice"}, r.pool)
}

func TestRaffleJoinClosed(t *testing.T) {
	r := &fakeRaffle{}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "join", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "no raffle")
	assert.Empty(t, r.pool)
}

func TestRaffleJoinTwiceKeepsOneEntry(t *testing.T) {
	r := &fakeRaffle{open: true, pool: []string{"bob"}}
	m := Raffle(raffleDeps(r))

	runQueue(t, m, "join", queueCtx("alice", ""), "")
	out := runQueue(t, m, "join", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "already")
	assert.Equal(t, []string{"bob", "alice"}, r.pool)
}

// --- open ---

func TestRaffleOpenRequiresMod(t *testing.T) {
	r := &fakeRaffle{}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("alice", ""), "open")
	assert.Empty(t, out)
	assert.False(t, r.open)
}

func TestRaffleOpenDefaults(t *testing.T) {
	r := &fakeRaffle{}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "open")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "!join")
	assert.True(t, r.open)
	// No explicit winner count: 0 means "the store's default" downstream.
	assert.Zero(t, r.lastSpec.Winners)
}

func TestRaffleOpenAlreadyRunning(t *testing.T) {
	r := &fakeRaffle{open: true}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "open 30")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "already")
}

// Reminder cadence parsing: absent arg leaves the store default (zero), a
// positive minute count passes through, and an explicit zero/negative
// disables — the store sees a negative duration for that.
func TestRaffleOpenReminderArgs(t *testing.T) {
	cases := []struct {
		args string
		want time.Duration
	}{
		{"", 0},
		{"10", 0},
		{"10 2", 0},
		{"10 2 3", 3 * time.Minute},
		{"10 2 0", -time.Second},
	}
	for _, tc := range cases {
		r := &fakeRaffle{}
		m := Raffle(raffleDeps(r))
		runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "open "+tc.args)
		assert.Equal(t, tc.want, r.lastSpec.Remind, "open %q", tc.args)
		assert.True(t, r.open)
	}
}

// --- draw/close/cancel/status/winner ---

func TestRaffleDrawAnnouncesWinners(t *testing.T) {
	r := &fakeRaffle{
		open: true,
		drawResult: &engine.RaffleResult{
			Winners: []string{"alice", "zoe"}, Entrants: 10,
		},
	}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "draw 2")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@alice, @zoe")
	assert.Contains(t, out[0].Text, "2 winner(s) from 10")
	assert.False(t, r.open)
}

func TestRaffleDrawEmptyPool(t *testing.T) {
	r := &fakeRaffle{open: true, drawResult: &engine.RaffleResult{}}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "close")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "No one entered")
}

func TestRaffleDrawNoneRunning(t *testing.T) {
	r := &fakeRaffle{}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "draw")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "No raffle is running")
}

func TestRaffleCancel(t *testing.T) {
	r := &fakeRaffle{open: true}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "cancel")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "cancelled")
	assert.False(t, r.open)
}

func TestRaffleStatusOpenAndClosed(t *testing.T) {
	r := &fakeRaffle{open: true, pool: []string{"a", "b"}}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "2 entered")

	r.open = false
	out = runQueue(t, m, "raffle", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "No raffle")
}

func TestWinnerRecall(t *testing.T) {
	r := &fakeRaffle{lastFound: true, lastResult: &engine.RaffleResult{Winners: []string{"alice"}, Entrants: 5}}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "winner", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "@alice")
}

func TestWinnerRecallShowsConfirmedClaims(t *testing.T) {
	r := &fakeRaffle{lastFound: true, lastResult: &engine.RaffleResult{
		Winners: []string{"alice", "zoe"}, Entrants: 5, Claims: []string{"alice"},
	}}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "winner", queueCtx("bob", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "1/2 confirmed")
}

// --- claim ---

func TestClaimConfirmedOnce(t *testing.T) {
	claimed := false
	r := &fakeRaffle{claimScript: func(string) engine.RaffleClaim {
		if claimed {
			return engine.ClaimAlready
		}
		claimed = true
		return engine.ClaimOk
	}}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "claim", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "confirmed")

	out = runQueue(t, m, "claim", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "already")
}

func TestClaimNotWinnerGetsNoPrizeLine(t *testing.T) {
	r := &fakeRaffle{claimScript: func(string) engine.RaffleClaim { return engine.ClaimNone }}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "claim", queueCtx("eve", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "no raffle prize")
	assert.Contains(t, out[0].Text, "!claim")
}

func TestClaimLate(t *testing.T) {
	r := &fakeRaffle{claimScript: func(string) engine.RaffleClaim { return engine.ClaimLate }}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "claim", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "window")
}

func TestWinnerNoneYet(t *testing.T) {
	r := &fakeRaffle{}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "winner", queueCtx("alice", ""), "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "No raffle has been drawn")
}

// --- customizable reply templates ---

func TestRaffleJoinCustomTemplate(t *testing.T) {
	r := &fakeRaffle{open: true}
	m := Raffle(raffleDeps(r))

	c := queueCtx("alice", "")
	c.Config = []byte(`{"joinMessage":"welcome {user}! {count} in so far"}`)
	out := runQueue(t, m, "join", c, "")
	require.Len(t, out, 1)
	assert.Equal(t, "welcome alice! 1 in so far", out[0].Text)
}

func TestRaffleWonCustomTemplate(t *testing.T) {
	r := &fakeRaffle{drawResult: &engine.RaffleResult{Winners: []string{"alice"}, Entrants: 7}}
	m := Raffle(raffleDeps(r))

	c := queueCtx("mod", "moderator")
	c.Config = []byte(`{"wonMessage":"{targets} takes it! {count} of {entrants}"}`)
	out := runQueue(t, m, "raffle", c, "draw")
	require.Len(t, out, 1)
	assert.Equal(t, "@alice takes it! 1 of 7", out[0].Text)
}

// A blank custom template falls back to the localized default; the status
// readout is never customizable, so a stray config key cannot alter it.
func TestRaffleStatusIgnoresConfig(t *testing.T) {
	r := &fakeRaffle{open: true, pool: []string{"a"}}
	m := Raffle(raffleDeps(r))

	c := queueCtx("alice", "")
	c.Config = []byte(`{"joinMessage":"custom"}`)
	out := runQueue(t, m, "raffle", c, "")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "1 entered")
}

// --- inert + usage ---

func TestRaffleNilStoreInert(t *testing.T) {
	m := Raffle(raffleDeps(nil))
	assert.Empty(t, runQueue(t, m, "join", queueCtx("alice", ""), ""))
	assert.Empty(t, runQueue(t, m, "raffle", queueCtx("mod", "moderator"), "open"))
}

func TestRaffleUnknownSubGetsUsage(t *testing.T) {
	r := &fakeRaffle{}
	m := Raffle(raffleDeps(r))

	out := runQueue(t, m, "raffle", queueCtx("alice", ""), "gimmick")
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "!raffle")
}

// The registry must give this module's !join priority over the queue module's:
// registration order in All() decides, so the ordering assertion lives here
// where a future reorder fails loudly instead of silently rerouting joins.
func TestRaffleOwnsStandaloneJoinOverQueue(t *testing.T) {
	deps := func() engine.Deps { return engine.Deps{Log: zap.NewNop()} }
	mods := All(deps())
	raffleIdx, queueIdx := -1, -1
	for i, mod := range mods {
		switch mod.Name {
		case raffleModuleName:
			raffleIdx = i
		case queueModuleName:
			queueIdx = i
		}
	}
	require.NotEqual(t, -1, raffleIdx)
	require.NotEqual(t, -1, queueIdx)
	assert.Less(t, raffleIdx, queueIdx, "raffle must register before queue to own !join")
}
