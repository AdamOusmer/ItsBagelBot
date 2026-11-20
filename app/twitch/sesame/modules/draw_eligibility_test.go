// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/domain/event/lane"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type drawJoinPorts struct{ draw.Payer }

func (drawJoinPorts) Announce(context.Context, draw.Notice) {}

type drawEligibilityData struct {
	standingsFailures int
	followCalls       int
	followErr         error
	snapshotID        string
}

func (d *drawEligibilityData) Snapshot(_ context.Context, req eligibility.StandingLookup) (loyaltyrpc.BalanceSnapshot, error) {
	d.snapshotID = req.SnapshotID
	if d.standingsFailures > 0 {
		d.standingsFailures--
		return loyaltyrpc.BalanceSnapshot{}, errors.New("standings unavailable")
	}
	return loyaltyrpc.BalanceSnapshot{Points: 40, Found: true}, nil
}

func (d *drawEligibilityData) FollowedAt(context.Context, uint64, uint64) (time.Time, bool, error) {
	d.followCalls++
	return time.Time{}, false, d.followErr
}

type drawEligibilityCase struct {
	name    string
	kind    draw.Kind
	rules   eligibility.Rules
	config  codec.RawMessage
	data    drawEligibilityData
	calls   []drawChatCall
	err     string
	results drawEligibilityResult
}

type drawEligibilityResult struct {
	entries, refused, refusalCalls, follows, batched int
	replies                                          []string
	weight                                           int64
}

func TestDrawEligibilityProcess(t *testing.T) {
	join := drawChatCall{command: "join", viewer: "7"}
	check := drawChatCall{command: "check", viewer: "7"}
	rejoin := []drawChatCall{join, join}
	lookupFailed := "@viewer eligibility could not be checked. Try again shortly."
	pointsRefusal := "@viewer you need 100 points. You had 40 when entries opened."
	joinPassed := "@viewer you meet the entry requirements. "
	cases := []drawEligibilityCase{
		{name: "free entry notice is shared by both aliases", calls: []drawChatCall{{command: "enter", viewer: "7"}, {command: "ticket", viewer: "7"}},
			results: drawEligibilityResult{entries: 1, weight: 100, replies: []string{"Entry is free."}}},
		{name: "refused once", rules: eligibility.Rules{Role: eligibility.Sub}, calls: rejoin,
			results: drawEligibilityResult{refused: 1, refusalCalls: 2, batched: 1}},
		{name: "refusal disabled", rules: eligibility.Rules{Role: eligibility.Sub}, config: codec.RawMessage(`{"refusalEnabled":false}`),
			calls: rejoin, results: drawEligibilityResult{refused: 1, refusalCalls: 2}},
		{name: "invalid refusal config propagates after recording", rules: eligibility.Rules{Role: eligibility.Sub},
			config: codec.RawMessage(`{"refusalEnabled":27}`), calls: []drawChatCall{join}, err: "invalid refusalEnabled",
			results: drawEligibilityResult{refused: 1, refusalCalls: 1}},
		{name: "lookup failures have their own dedup even with refusal lines disabled", rules: eligibility.Rules{MinPoints: 100},
			config: codec.RawMessage(`{"refusalEnabled":false}`), data: drawEligibilityData{standingsFailures: 2},
			calls: rejoin, results: drawEligibilityResult{replies: []string{lookupFailed}}},
		{name: "lookup failure leaves the rule refusal available", rules: eligibility.Rules{MinPoints: 100},
			data: drawEligibilityData{standingsFailures: 1}, calls: rejoin,
			results: drawEligibilityResult{refused: 1, refusalCalls: 1, replies: []string{lookupFailed}, batched: 1}},
		{name: "lookup failure can recover and admit", rules: eligibility.Rules{MinPoints: 40},
			data: drawEligibilityData{standingsFailures: 1}, calls: rejoin,
			results: drawEligibilityResult{entries: 1, weight: 100, replies: []string{lookupFailed}}},
		{name: "own bot messages are ignored", calls: []drawChatCall{{command: "join", viewer: "99"}}},
		{name: "giveaway excludes its broadcaster", calls: []drawChatCall{{command: "join"}},
			results: drawEligibilityResult{refused: 1, refusalCalls: 1, batched: 1}},
		{name: "raffle admits its broadcaster", kind: draw.KindRaffle, calls: []drawChatCall{{command: "join"}},
			results: drawEligibilityResult{entries: 1, weight: 100}},
		{name: "check reports private cutoff before draw rules", rules: eligibility.Rules{MinPoints: 100, FollowersOnly: true},
			calls: []drawChatCall{check}, results: drawEligibilityResult{
				replies: []string{pointsRefusal + " We check these when we draw the winners: channel follow."}}},
		{name: "check uses a per-viewer cooldown and checks follow", rules: eligibility.Rules{FollowersOnly: true},
			calls:   []drawChatCall{check, check, {command: "check", viewer: "8"}},
			results: drawEligibilityResult{follows: 2, replies: []string{joinPassed + "@viewer a channel follow could not be verified.", joinPassed + "@viewer a channel follow could not be verified."}}},
		{name: "check reports missing followers scope", rules: eligibility.Rules{FollowersOnly: true},
			data: drawEligibilityData{followErr: eligibility.ErrFollowScope}, calls: []drawChatCall{check},
			results: drawEligibilityResult{follows: 1, replies: []string{joinPassed + "@viewer the bot needs permission to read this channel's followers."}}},
		{name: "check confirms eligibility without entering", calls: []drawChatCall{check},
			results: drawEligibilityResult{replies: []string{"@viewer you meet the entry requirements."}}},
	}
	for _, tc := range []struct {
		name   string
		badges []lane.Badge
		weight int64
	}{
		{"subscriber", []lane.Badge{{SetID: "subscriber"}}, 200},
		{"VIP with founder badge", []lane.Badge{{SetID: "vip"}, {SetID: "founder"}}, 200},
		{"VIP without subscription", []lane.Badge{{SetID: "vip"}}, 100},
		{"moderator without subscription", []lane.Badge{{SetID: "moderator"}}, 100},
	} {
		cases = append(cases, drawEligibilityCase{
			name:    "subscriber luck for " + tc.name,
			rules:   eligibility.Rules{SubLuckPercent: 200, FollowersOnly: true},
			calls:   []drawChatCall{{command: "join", viewer: "7", badges: tc.badges}},
			results: drawEligibilityResult{entries: 1, weight: tc.weight},
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc drawEligibilityCase) run(t *testing.T) {
	vc := valkeytest.Real(t)
	data := &tc.data
	checker := eligibility.NewChecker(eligibility.Ports{Standings: data, Follows: data, Now: func() time.Time {
		return time.Date(2026, 11, 19, 20, 14, 0, 0, time.UTC)
	}})
	t.Cleanup(checker.Close)
	ports := drawJoinPorts{}
	store := &fakeDraw{DrawStore: draw.NewStore(vc, draw.Config{
		Verifier: checker, Payer: ports, Announcer: ports, Winners: draw.DiscardWinnerRecords{},
		Phases: draw.IgnorePhaseChanges{},
	})}
	pub := &variablePublisher{}
	deps := engine.Deps{Draw: store, Eligibility: checker, Pub: pub,
		Proj: &fakeProj{modules: []projection.ModuleView{{Name: engine.GiveawayModuleName, Configs: tc.config}}},
		Live: &fakeLive{live: true}, Greet: &fakeGreet{}, Cooldown: engine.NewValkeyCooldown(vc),
		Special: engine.NewSpecialSet(""), Log: zap.NewNop()}
	p := engine.NewPipeline(deps, engine.NewRegistry(deps.Log, Draw(deps)), engine.Config{OutgressStandard: "outgress.standard", BotID: "99"})
	t.Cleanup(p.Close)
	round := tc.open(t, store)
	tc.process(t, p, round.Channel)
	if tc.rules.NeedsStandings() {
		require.Equal(t, "private-snapshot", data.snapshotID)
	}
	rounds, err := store.Live(t.Context(), round.Channel)
	require.NoError(t, err)
	require.Len(t, rounds, 1)
	require.Equal(t, tc.results, drawEligibilityResult{
		entries: rounds[0].Entries, refused: int(rounds[0].Refused), refusalCalls: store.refusals,
		follows: data.followCalls, batched: store.batched, replies: drawReplies(t, pub), weight: store.joined.Weight,
	})
}

func (tc drawEligibilityCase) process(t *testing.T, pipe *engine.Pipeline, channel draw.ChannelID) {
	t.Helper()
	for _, call := range tc.calls {
		err := call.process(t, pipe, channel)
		if tc.err == "" {
			require.NoError(t, err)
			continue
		}
		require.ErrorContains(t, err, tc.err)
	}
}

func (tc drawEligibilityCase) open(t *testing.T, store engine.DrawStore) draw.Round {
	t.Helper()
	raw, err := codec.Marshal(tc.rules)
	require.NoError(t, err)
	channel := draw.ChannelID(time.Now().UnixNano())
	terms := draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "Prize"}, Rules: raw, ClosesAt: time.Now().Add(time.Minute)}
	if tc.rules.NeedsStandings() {
		terms.SnapshotID = "private-snapshot"
	}
	if tc.kind == draw.KindRaffle {
		terms.Kind, terms.Prize = draw.KindRaffle, draw.Prize{Points: 100}
	}
	round, status, err := store.Open(t.Context(), draw.OpenRequest{Channel: channel, Terms: terms})
	require.NoError(t, err)
	require.Equal(t, draw.Opened, status)
	t.Cleanup(func() {
		_, err := store.Cancel(context.Background(), draw.ModAction{Channel: channel, Round: round.ID})
		require.NoError(t, err)
	})
	return round
}
