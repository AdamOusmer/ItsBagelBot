// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeRaffle struct {
	open       bool
	pool       []string
	lastSpec   engine.RaffleOpenSpec
	drawResult *engine.RaffleResult
	lastResult *engine.RaffleResult
	lastFound  bool
	claim      engine.RaffleClaim
	err        error
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

func (f *fakeRaffle) Join(_ context.Context, _ uint64, userID string) (engine.RaffleEntry, error) {
	entry := engine.RaffleEntry{Open: f.open, Entrants: int64(len(f.pool))}
	for _, p := range f.pool {
		if p == userID {
			return entry, nil
		}
	}
	if !f.open {
		return entry, nil
	}
	f.pool = append(f.pool, userID)
	entry.Entrants = int64(len(f.pool))
	entry.Joined = true
	return entry, nil
}

func (f *fakeRaffle) Status(_ context.Context, _ uint64) (engine.RaffleStatus, error) {
	return engine.RaffleStatus{Open: f.open, Entrants: int64(len(f.pool)), SecondsLeft: 600}, f.err
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

func (f *fakeRaffle) Claim(context.Context, uint64, string) (engine.RaffleClaim, error) {
	return f.claim, nil
}

func (f *fakeRaffle) StartExpiryWatcher(context.Context) {}

func raffleDeps(r engine.RaffleStore) engine.Deps {
	return engine.Deps{Raffle: r, Log: zap.NewNop()}
}

func TestRaffleJoin(t *testing.T) {
	cases := []struct {
		name     string
		config   string
		raffle   fakeRaffle
		exact    string
		contains []string
		pool     []string
	}{
		{name: "joining an open raffle counts the entry", raffle: fakeRaffle{open: true}, contains: []string{"@alice", "1 entered"}, pool: []string{"alice"}},
		{name: "joining a closed raffle is refused", contains: []string{"no raffle"}},
		{name: "joining twice keeps one entry", raffle: fakeRaffle{open: true, pool: []string{"bob", "alice"}}, contains: []string{"already"}, pool: []string{"bob", "alice"}},
		{name: "the join template fills the count", config: `{"joinMessage":"welcome {user}! {count} in so far"}`, raffle: fakeRaffle{open: true},
			exact: "welcome alice! 1 in so far", pool: []string{"alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.raffle
			out := runChat(t, Raffle(raffleDeps(&r)), withConfig(chatCtx("42", "alice"), tc.config), "!join")
			require.Len(t, out, 1)
			assertText(t, out[0].Text, textWant{tc.exact, tc.contains, nil})
			assert.Equal(t, tc.pool, r.pool)
		})
	}
}

type raffleOpen int

const (
	raffleOpenUnchecked raffleOpen = iota
	raffleOpened
	raffleClosed
)

func TestRaffleChat(t *testing.T) {
	won := &engine.RaffleResult{Winners: []string{"alice", "zoe"}, Entrants: 10}
	cases := []struct {
		name     string
		text     string
		who      string
		badge    string
		config   string
		raffle   fakeRaffle
		silent   bool
		exact    string
		contains []string
		open     raffleOpen
	}{
		{name: "a viewer cannot open a raffle", text: "!raffle open", who: "alice", silent: true, open: raffleClosed},
		{name: "a mod opens a raffle with the defaults", text: "!raffle open", who: "mod", badge: "moderator", contains: []string{"!join"}, open: raffleOpened},
		{name: "opening a running raffle is refused", text: "!raffle open 30", who: "mod", badge: "moderator", raffle: fakeRaffle{open: true}, contains: []string{"already"}},
		{name: "drawing announces the winners", text: "!raffle draw 2", who: "mod", badge: "moderator", raffle: fakeRaffle{open: true, drawResult: won},
			contains: []string{"@alice, @zoe", "2 winner(s) from 10"}, open: raffleClosed},
		{name: "closing an empty raffle says no one entered", text: "!raffle close", who: "mod", badge: "moderator",
			raffle: fakeRaffle{open: true, drawResult: &engine.RaffleResult{}}, contains: []string{"No one entered"}},
		{name: "drawing with nothing running says so", text: "!raffle draw", who: "mod", badge: "moderator", contains: []string{"No raffle is running"}},
		{name: "the won template fills the winners", text: "!raffle draw", who: "mod", badge: "moderator", config: `{"wonMessage":"{targets} takes it! {count} of {entrants}"}`,
			raffle: fakeRaffle{drawResult: &engine.RaffleResult{Winners: []string{"alice"}, Entrants: 7}}, exact: "@alice takes it! 1 of 7"},
		{name: "cancelling closes the raffle", text: "!raffle cancel", who: "mod", badge: "moderator", raffle: fakeRaffle{open: true}, contains: []string{"cancelled"}, open: raffleClosed},
		{name: "status reports the entrants", text: "!raffle", who: "alice", raffle: fakeRaffle{open: true, pool: []string{"a", "b"}}, contains: []string{"2 entered"}},
		{name: "status reports a closed raffle", text: "!raffle", who: "alice", contains: []string{"No raffle"}},
		{name: "status ignores the join template", text: "!raffle", who: "alice", config: `{"joinMessage":"custom"}`, raffle: fakeRaffle{open: true, pool: []string{"a"}},
			contains: []string{"1 entered"}},
		{name: "an unknown subcommand prints usage", text: "!raffle gimmick", who: "alice", contains: []string{"!raffle"}},
		{name: "winner recalls the last draw", text: "!winner", who: "alice", raffle: fakeRaffle{lastFound: true, lastResult: &engine.RaffleResult{Winners: []string{"alice"}, Entrants: 5}},
			contains: []string{"@alice"}},
		{name: "winner shows the confirmed claims", text: "!winner", who: "bob",
			raffle:   fakeRaffle{lastFound: true, lastResult: &engine.RaffleResult{Winners: []string{"alice", "zoe"}, Entrants: 5, Claims: []string{"alice"}}},
			contains: []string{"1/2 confirmed"}},
		{name: "winner before any draw says so", text: "!winner", who: "alice", contains: []string{"No raffle has been drawn"}},
		{name: "a winner's claim is confirmed", text: "!claim", who: "alice", raffle: fakeRaffle{claim: engine.ClaimOk}, contains: []string{"confirmed"}},
		{name: "a second claim is already confirmed", text: "!claim", who: "alice", raffle: fakeRaffle{claim: engine.ClaimAlready}, contains: []string{"already"}},
		{name: "a non winner gets no prize line", text: "!claim", who: "eve", raffle: fakeRaffle{claim: engine.ClaimNone}, contains: []string{"no raffle prize", "!claim"}},
		{name: "a late claim cites the window", text: "!claim", who: "alice", raffle: fakeRaffle{claim: engine.ClaimLate}, contains: []string{"window"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.raffle
			out := runChat(t, Raffle(raffleDeps(&r)), withConfig(chatCtx("42", tc.who, tc.badge), tc.config), tc.text)
			if tc.silent {
				assert.Empty(t, out)
			} else {
				require.Len(t, out, 1)
				assertText(t, out[0].Text, textWant{tc.exact, tc.contains, nil})
			}
			switch tc.open {
			case raffleOpened:
				assert.True(t, r.open)
			case raffleClosed:
				assert.False(t, r.open)
			}
		})
	}
}

func TestRaffleOpenArgs(t *testing.T) {
	cases := []struct {
		args string
		want engine.RaffleOpenSpec
	}{
		{"", engine.RaffleOpenSpec{OpenedBy: "mod"}},
		{"10", engine.RaffleOpenSpec{OpenedBy: "mod", Duration: 10 * time.Minute}},
		{"10 2", engine.RaffleOpenSpec{OpenedBy: "mod", Duration: 10 * time.Minute, Winners: 2}},
		{"10 2 3", engine.RaffleOpenSpec{OpenedBy: "mod", Duration: 10 * time.Minute, Winners: 2, Remind: 3 * time.Minute}},
		{"10 2 0", engine.RaffleOpenSpec{OpenedBy: "mod", Duration: 10 * time.Minute, Winners: 2, Remind: -time.Second}},
	}
	for _, tc := range cases {
		r := &fakeRaffle{}
		runChat(t, Raffle(raffleDeps(r)), chatCtx("42", "mod", "moderator"), "!raffle open "+tc.args)
		assert.Equal(t, tc.want, r.lastSpec, "open %q", tc.args)
		assert.True(t, r.open)
	}
}

func TestRaffleStaysSilentWithoutAStore(t *testing.T) {
	m := Raffle(raffleDeps(nil))
	assert.Empty(t, runChat(t, m, chatCtx("42", "alice"), "!join"))
	assert.Empty(t, runChat(t, m, chatCtx("9", "mod", "moderator"), "!raffle open"))
}
