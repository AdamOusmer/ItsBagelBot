// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
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
		{name: "the raffle subcommand joins too", text: "!raffle join", config: raffleJoinOn, raffle: fakeRaffle{open: true}, contains: []string{"1 entered"}, pool: []string{"alice"}},
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
			out := runChat(t, Raffle(d), c, text)
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

type raffleOpen int

const (
	raffleOpenUnchecked raffleOpen = iota
	raffleOpened
	raffleClosed
)

type raffleChatCase struct {
	name      string
	text      string
	who       string
	badge     string
	config    string
	raffle    fakeRaffle
	silent    bool
	unclaimed bool
	exact     string
	contains  []string
	excludes  []string
	open      raffleOpen
}

func (tc raffleChatCase) run(t *testing.T) {
	r := tc.raffle
	out := runChat(t, Raffle(raffleDeps(&r)), withConfig(chatCtx("42", tc.who, tc.badge), tc.config), tc.text)
	if tc.silent {
		assert.Empty(t, out)
	} else {
		require.Len(t, out, 1)
		assertText(t, out[0].Text, textWant{tc.exact, tc.contains, tc.excludes})
	}
	if tc.unclaimed {
		assert.Empty(t, r.claimedBy)
	}
	switch tc.open {
	case raffleOpened:
		assert.True(t, r.open)
	case raffleClosed:
		assert.False(t, r.open)
	}
}

var (
	raffleWon  = &engine.RaffleResult{Winners: winners("alice", "zoe"), Entrants: 10}
	rafflePaid = &engine.RaffleResult{Winners: []engine.RaffleWinner{{ID: "1", Login: "alice", Points: 1000}}, Entrants: 4, Prize: 1000, Payout: engine.RafflePayoutPaid}
)

func TestRaffleChat(t *testing.T) {
	won, paid := raffleWon, rafflePaid
	cases := []raffleChatCase{
		{name: "a viewer cannot open a raffle", text: "!raffle 500", who: "alice", silent: true, open: raffleClosed},
		{name: "a mod opens a raffle with the defaults", text: "!raffle", who: "mod", badge: "moderator", contains: []string{"!join"}, open: raffleOpened},
		{name: "opening a running raffle is refused", text: "!raffle 500 30", who: "mod", badge: "moderator", raffle: fakeRaffle{open: true}, contains: []string{"already"}},
		{name: "the old open subcommand prints usage", text: "!raffle open", who: "mod", badge: "moderator", contains: []string{"Usage"}, open: raffleClosed},
		{name: "a viewer cannot start a single raffle", text: "!sraffle", who: "alice", silent: true, open: raffleClosed},
		{name: "a viewer cannot start a multi raffle", text: "!mraffle", who: "alice", silent: true, open: raffleClosed},
		{name: "a viewer cannot cancel", text: "!cancelraffle", who: "alice", raffle: fakeRaffle{open: true}, silent: true, open: raffleOpened},
		{name: "cancelraffle cancels", text: "!cancelraffle", who: "mod", badge: "moderator", raffle: fakeRaffle{open: true}, contains: []string{"cancelled"}, open: raffleClosed},
		{name: "cancelraffle with nothing running says so", text: "!cancelraffle", who: "mod", badge: "moderator", contains: []string{"No raffle is running"}},
		{name: "the singleraffle alias opens", text: "!singleraffle 50", who: "mod", badge: "moderator", contains: []string{"!join"}, open: raffleOpened},
		{name: "the multiraffle alias opens", text: "!multiraffle 50", who: "mod", badge: "moderator", contains: []string{"!join"}, open: raffleOpened},
		{name: "a seconds raffle announces seconds", text: "!raffle 100 30", who: "mod", badge: "moderator", exact: "Raffle is LIVE! Type !join to enter. Drawing in 30 sec!", open: raffleOpened},
		{name: "a whole minute announces minutes", text: "!raffle 100 120", who: "mod", badge: "moderator", exact: "Raffle is LIVE! Type !join to enter. Drawing in 2 min!", open: raffleOpened},
		{name: "junk amount prints usage", text: "!raffle lots", who: "mod", badge: "moderator", contains: []string{"Usage"}, open: raffleClosed},
		{name: "junk seconds prints usage", text: "!mraffle 100 soon", who: "mod", badge: "moderator", contains: []string{"Usage"}, open: raffleClosed},
		{name: "a viewer gets no reply to junk", text: "!raffle lots", who: "alice", silent: true},
		{name: "status subcommand reports the entrants", text: "!raffle status", who: "alice", raffle: fakeRaffle{open: true, pool: []string{"a"}}, contains: []string{"1 entered"}},
		{name: "a mod can read the status", text: "!raffle status", who: "mod", badge: "moderator", contains: []string{"No raffle"}},
		{name: "drawing announces the winners", text: "!raffle draw 2", who: "mod", badge: "moderator", raffle: fakeRaffle{open: true, drawResult: won},
			contains: []string{"@alice, @zoe", "2 winner(s) from 10"}, open: raffleClosed},
		{name: "closing an empty raffle says no one entered", text: "!raffle close", who: "mod", badge: "moderator",
			raffle: fakeRaffle{open: true, drawResult: &engine.RaffleResult{}}, contains: []string{"No one entered"}},
		{name: "drawing with nothing running says so", text: "!raffle draw", who: "mod", badge: "moderator", contains: []string{"No raffle is running"}},
		{name: "the won template fills the winners", text: "!raffle draw", who: "mod", badge: "moderator", config: `{"wonMessage":"{targets} takes it! {count} of {entrants}"}`,
			raffle: fakeRaffle{drawResult: &engine.RaffleResult{Winners: winners("alice"), Entrants: 7}}, exact: "@alice takes it! 1 of 7"},
		{name: "a paid draw names the points", text: "!raffle draw", who: "mod", badge: "moderator", raffle: fakeRaffle{drawResult: paid},
			contains: []string{"@alice", "1000 points per winner"}},
		{name: "cancelling closes the raffle", text: "!raffle cancel", who: "mod", badge: "moderator", raffle: fakeRaffle{open: true}, contains: []string{"cancelled"}, open: raffleClosed},
		{name: "status reports the entrants", text: "!raffle", who: "alice", raffle: fakeRaffle{open: true, pool: []string{"a", "b"}}, contains: []string{"2 entered"}},
		{name: "status reports a closed raffle", text: "!raffle", who: "alice", contains: []string{"No raffle"}},
		{name: "status ignores the join template", text: "!raffle", who: "alice", config: `{"joinMessage":"custom"}`, raffle: fakeRaffle{open: true, pool: []string{"a"}},
			contains: []string{"1 entered"}},
		{name: "an unknown subcommand is silent for a viewer", text: "!raffle gimmick", who: "alice", silent: true},
		{name: "winner recalls the last draw", text: "!winner", who: "alice", raffle: fakeRaffle{lastFound: true, lastResult: &engine.RaffleResult{Winners: winners("alice"), Entrants: 5}},
			contains: []string{"@alice"}},
		{name: "winner shows the confirmed claims", text: "!winner", who: "bob",
			raffle:   fakeRaffle{lastFound: true, lastResult: &engine.RaffleResult{Winners: winners("alice", "zoe"), Entrants: 5, Claims: []string{"id-alice"}}},
			contains: []string{"1/2 confirmed"}},
		{name: "winner before any draw says so", text: "!winner", who: "alice", contains: []string{"No raffle has been drawn"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func TestRaffleLineToggles(t *testing.T) {
	won, paid := raffleWon, rafflePaid
	cases := []raffleChatCase{
		{name: "claim is silent and records nothing by default", text: "!claim", who: "alice", raffle: fakeRaffle{claim: engine.ClaimOk}, silent: true, unclaimed: true},
		{name: "claim stays off for other values", text: "!claim", who: "alice", config: `{"claimEnabled":"off"}`, raffle: fakeRaffle{claim: engine.ClaimOk}, silent: true, unclaimed: true},
		{name: "a winner's claim is confirmed", text: "!claim", who: "alice", config: raffleClaimOn, raffle: fakeRaffle{claim: engine.ClaimOk}, contains: []string{"confirmed"}},
		{name: "the claim confirmation line can be turned off", text: "!claim", who: "alice", config: `{"claimEnabled":"on","claimOkEnabled":"off"}`, raffle: fakeRaffle{claim: engine.ClaimOk}, silent: true},
		{name: "a second claim is already confirmed", text: "!claim", who: "alice", config: raffleClaimOn, raffle: fakeRaffle{claim: engine.ClaimAlready}, contains: []string{"already"}},
		{name: "a non winner gets no prize line", text: "!claim", who: "eve", config: raffleClaimOn, raffle: fakeRaffle{claim: engine.ClaimNone}, contains: []string{"no raffle prize", "!claim"}},
		{name: "a late claim cites the window", text: "!claim", who: "alice", config: raffleClaimOn, raffle: fakeRaffle{claim: engine.ClaimLate}, contains: []string{"window"}},
		{name: "opening can be silenced", text: "!raffle", who: "mod", badge: "moderator", config: `{"openedEnabled":"off"}`, silent: true, open: raffleOpened},
		{name: "a draw without the won line posts no loyalty note", text: "!raffle draw", who: "mod", badge: "moderator", config: `{"wonEnabled":"off"}`,
			raffle: fakeRaffle{drawResult: &engine.RaffleResult{Winners: winners("alice"), Entrants: 4, Prize: 1000, Payout: engine.RafflePayoutLoyaltyOff}},
			silent: true},
		{name: "a draw skipped for loyalty posts the plain won line", text: "!raffle draw", who: "mod", badge: "moderator",
			raffle:   fakeRaffle{drawResult: &engine.RaffleResult{Winners: winners("alice"), Entrants: 4, Prize: 1000, Payout: engine.RafflePayoutLoyaltyOff}},
			contains: []string{"congratulations"}, excludes: []string{"Loyalty is off", "1000"}},
		{name: "a draw with the won line off and no note is silent", text: "!raffle draw", who: "mod", badge: "moderator", config: `{"wonEnabled":"false"}`,
			raffle: fakeRaffle{drawResult: paid}, silent: true},
		{name: "the won line omits !claim when claim is off", text: "!raffle draw", who: "mod", badge: "moderator", raffle: fakeRaffle{drawResult: won},
			contains: []string{"congratulations"}, excludes: []string{"!claim"}},
		{name: "the won line asks for !claim when claim is on", text: "!raffle draw", who: "mod", badge: "moderator", config: raffleClaimOn, raffle: fakeRaffle{drawResult: won},
			contains: []string{"!claim", "your prize"}},
		{name: "a paid won line asks for !claim when claim is on", text: "!raffle draw", who: "mod", badge: "moderator", config: raffleClaimOn, raffle: fakeRaffle{drawResult: paid},
			contains: []string{"1000 points", "!claim"}},
		{name: "a custom claim prize is named", text: "!raffle draw", who: "mod", badge: "moderator", config: `{"claimEnabled":"on","claimPrize":"a signed poster"}`, raffle: fakeRaffle{drawResult: won},
			contains: []string{"a signed poster"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func TestRaffleStartArgs(t *testing.T) {
	spec := func(prize int64, secs int, multi bool) engine.RaffleOpenSpec {
		return engine.RaffleOpenSpec{OpenedBy: "mod", Duration: time.Duration(secs) * time.Second, Prize: prize, Multi: multi}
	}
	cases := []struct {
		text   string
		config string
		want   engine.RaffleOpenSpec
	}{
		{"!raffle", "", spec(1000, 60, false)},
		{"!raffle 500", "", spec(500, 60, false)},
		{"!raffle 5k", "", spec(5000, 60, false)},
		{"!raffle 1.5k 90", "", spec(1500, 90, false)},
		{"!raffle 2M", "", spec(1_000_000, 60, false)},
		{"!raffle 99999999m", "", spec(1_000_000, 60, false)},
		{"!raffle 0", "", spec(0, 60, false)},
		{"!raffle 100 2", "", spec(100, 2, false)},
		{"!sraffle 100 45", "", spec(100, 45, false)},
		{"!singleraffle 100", "", spec(100, 60, false)},
		{"!mraffle", "", spec(1000, 60, true)},
		{"!mraffle 2k 30", "", spec(2000, 30, true)},
		{"!multiraffle 2k", "", spec(2000, 60, true)},
		{"!raffle", `{"prizePoints":"250","durationSeconds":"20"}`, spec(250, 20, false)},
		{"!raffle", `{"durationSeconds":"junk"}`, spec(1000, 60, false)},
		{"!raffle 100", `{"durationSeconds":15}`, spec(100, 15, false)},
	}
	for _, tc := range cases {
		t.Run(tc.text+tc.config, func(t *testing.T) {
			r := &fakeRaffle{}
			d := raffleDeps(r)
			d.Proj = &fakeProj{modules: []projection.ModuleView{{Name: engine.LoyaltyModuleName, IsEnabled: true}}}
			runChat(t, Raffle(d), withConfig(chatCtx("42", "mod", "moderator"), tc.config), tc.text)
			assert.True(t, r.open)
			assert.Equal(t, tc.want, r.lastSpec)
		})
	}
}

func TestRaffleRejectsMalformedArgs(t *testing.T) {
	for _, text := range []string{"!raffle k", "!raffle 1.5", "!raffle -5", "!raffle 5%", "!raffle all", "!raffle 1e3", "!raffle 100 -1", "!raffle 100 1.5", "!mraffle abc"} {
		t.Run(text, func(t *testing.T) {
			r := &fakeRaffle{}
			out := runChat(t, Raffle(raffleDeps(r)), chatCtx("42", "mod", "moderator"), text)
			assert.False(t, r.open)
			require.Len(t, out, 1)
			assert.Contains(t, out[0].Text, "Usage")
		})
	}
}

func TestRaffleOpenPrizeFollowsLoyalty(t *testing.T) {
	loyaltyOn := []projection.ModuleView{{Name: engine.LoyaltyModuleName, IsEnabled: true}}
	cases := []struct {
		name      string
		config    string
		modules   []projection.ModuleView
		wantOpen  bool
		wantPrize int64
		contains  string
	}{
		{name: "the default prize opens with loyalty on", modules: loyaltyOn, wantOpen: true, wantPrize: 1000, contains: "!join"},
		{name: "a prize saved as text is honored", config: `{"prizePoints":"250"}`, modules: loyaltyOn, wantOpen: true, wantPrize: 250, contains: "!join"},
		{name: "a points raffle opens without points when loyalty is off", wantOpen: true, contains: "!join"},
		{name: "a zero prize runs without loyalty", config: `{"prizePoints":"0"}`, wantOpen: true, contains: "!join"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRaffle{}
			d := raffleDeps(r)
			d.Proj = &fakeProj{modules: tc.modules}
			out := runChat(t, Raffle(d), withConfig(chatCtx("42", "mod", "moderator"), tc.config), "!raffle")
			require.Len(t, out, 1)
			assert.Contains(t, out[0].Text, tc.contains)
			assert.Equal(t, tc.wantOpen, r.open)
			assert.Equal(t, tc.wantPrize, r.lastSpec.Prize)
		})
	}
}

func TestRaffleKeysEntriesAndClaimsByViewerID(t *testing.T) {
	r := &fakeRaffle{open: true}
	m := Raffle(raffleDeps(r))

	runChat(t, m, chatCtx("777", "Alice"), "!join")
	runChat(t, m, withConfig(chatCtx("777", "alice"), raffleClaimOn), "!claim")

	assert.Equal(t, []string{"777"}, r.joinedIDs)
	assert.Equal(t, []string{"alice"}, r.pool, "the login is kept lower-cased for mentions")
	assert.Equal(t, "777", r.claimedBy)
}

func TestRaffleStaysSilentWithoutAStore(t *testing.T) {
	m := Raffle(raffleDeps(nil))
	assert.Empty(t, runChat(t, m, chatCtx("42", "alice"), "!join"))
	assert.Empty(t, runChat(t, m, chatCtx("9", "mod", "moderator"), "!raffle"))
}
