// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"

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
	claimedBy  string
	joinedIDs  []string
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

func (f *fakeRaffle) Join(_ context.Context, _ uint64, entrant engine.RaffleEntrant) (engine.RaffleEntry, error) {
	entry := engine.RaffleEntry{Open: f.open, Entrants: int64(len(f.pool))}
	for _, p := range f.pool {
		if p == entrant.Login {
			return entry, nil
		}
	}
	if !f.open {
		return entry, nil
	}
	f.pool = append(f.pool, entrant.Login)
	f.joinedIDs = append(f.joinedIDs, entrant.ID)
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

func (f *fakeRaffle) Claim(_ context.Context, _ uint64, viewerID string) (engine.RaffleClaim, error) {
	f.claimedBy = viewerID
	return f.claim, nil
}

func winners(logins ...string) []engine.RaffleWinner {
	out := make([]engine.RaffleWinner, len(logins))
	for i, login := range logins {
		out[i] = engine.RaffleWinner{ID: "id-" + login, Login: login}
	}
	return out
}

func (f *fakeRaffle) StartExpiryWatcher(context.Context) {}

func raffleDeps(r engine.RaffleStore) engine.Deps {
	return engine.Deps{Raffle: r, Log: zap.NewNop()}
}

const (
	raffleJoinOn  = `{"joinEnabled":"on"}`
	raffleClaimOn = `{"claimEnabled":"on"}`
)

func TestRaffleJoin(t *testing.T) {
	openQueue := &fakeQueue{open: true}
	cases := []struct {
		name     string
		text     string
		config   string
		raffle   fakeRaffle
		queue    *fakeQueue
		queueOn  bool
		exact    string
		contains []string
		excludes []string
		pool     []string
		silent   bool
	}{
		{name: "joining an open raffle is silent by default", raffle: fakeRaffle{open: true}, silent: true, pool: []string{"alice"}},
		{name: "joining an open raffle counts the entry", config: raffleJoinOn, raffle: fakeRaffle{open: true}, contains: []string{"@alice", "1 entered"}, excludes: []string{"!queue join"}, pool: []string{"alice"}},
		{name: "joining a closed raffle is refused", contains: []string{"no raffle"}},
		{name: "the no raffle line can be turned off", config: `{"noRaffleEnabled":"off"}`, silent: true},
		{name: "the no raffle line stays on for any other value", config: `{"noRaffleEnabled":"on"}`, contains: []string{"no raffle"}},
		{name: "joining twice is silent by default", raffle: fakeRaffle{open: true, pool: []string{"bob", "alice"}}, silent: true, pool: []string{"bob", "alice"}},
		{name: "joining twice keeps one entry", config: `{"alreadyEnabled":"true"}`, raffle: fakeRaffle{open: true, pool: []string{"bob", "alice"}}, contains: []string{"already"}, pool: []string{"bob", "alice"}},
		{name: "the already line stays off when only join is on", config: raffleJoinOn, raffle: fakeRaffle{open: true, pool: []string{"alice"}}, silent: true, pool: []string{"alice"}},
		{name: "the join template fills the count", config: `{"joinEnabled":"on","joinMessage":"welcome {user}! {count} in so far"}`, raffle: fakeRaffle{open: true},
			exact: "welcome alice! 1 in so far", pool: []string{"alice"}},
		{name: "a join template stays silent while the line is off", config: `{"joinMessage":"welcome"}`, raffle: fakeRaffle{open: true}, silent: true, pool: []string{"alice"}},
		{name: "an open queue is pointed to", config: raffleJoinOn, raffle: fakeRaffle{open: true}, queue: openQueue, queueOn: true,
			contains: []string{"1 entered", "!queue join"}, pool: []string{"alice"}},
		{name: "a repeat entry also points to the queue", config: `{"alreadyEnabled":"on"}`, raffle: fakeRaffle{open: true, pool: []string{"alice"}}, queue: openQueue, queueOn: true,
			contains: []string{"already", "!queue join"}, pool: []string{"alice"}},
		{name: "a closed queue is not pointed to", config: raffleJoinOn, raffle: fakeRaffle{open: true}, queue: &fakeQueue{}, queueOn: true,
			excludes: []string{"!queue join"}, pool: []string{"alice"}},
		{name: "a disabled queue module is not pointed to", config: raffleJoinOn, raffle: fakeRaffle{open: true}, queue: openQueue,
			excludes: []string{"!queue join"}, pool: []string{"alice"}},
		{name: "a custom join template gets no queue hint", config: `{"joinEnabled":"on","joinMessage":"in!"}`, raffle: fakeRaffle{open: true}, queue: openQueue, queueOn: true,
			exact: "in!", pool: []string{"alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.raffle
			d := raffleDeps(&r)
			if tc.queue != nil {
				d.Queue = tc.queue
			}
			c := withConfig(chatCtx("42", "alice"), tc.config)
			c.ModuleLookup = func(name string) bool { return tc.queueOn && name == queueModuleName }
			text := tc.text
			if text == "" {
				text = "!join"
			}
			out := runRaffleChat(t, d, c, text)
			if tc.silent {
				assert.Empty(t, out)
			} else {
				require.Len(t, out, 1)
				assertText(t, out[0].Text, textWant{tc.exact, tc.contains, tc.excludes})
			}
			assert.Equal(t, tc.pool, r.pool)
		})
	}
}

func runRaffleChat(t *testing.T, d engine.Deps, c *module.Context, command string) []module.Output {
	t.Helper()
	switch command {
	case "!join", "!claim", "!winner":
		d.Proj = &fakeProj{modules: []projection.ModuleView{enabledVariables(raffleModuleName, string(c.Config))}}
		moduleOn := c.ModuleLookup
		c.ModuleLookup = func(name string) bool { return name == raffleModuleName || (moduleOn != nil && moduleOn(name)) }
		return runChat(t, Draw(d), c, command)
	default:
		return runChat(t, Raffle(d), c, command)
	}
}
