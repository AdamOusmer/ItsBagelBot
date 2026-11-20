// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/domain/event/lane"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type drawTestAnnouncer struct{}

func (drawTestAnnouncer) Announce(context.Context, draw.Notice) {}

type drawTestPayer struct{ draw.Payer }

type observedDrawStore struct {
	*draw.Store
	weight int64
}

func (s *observedDrawStore) Join(ctx context.Context, request draw.JoinRequest) (draw.JoinOutcome, error) {
	s.weight = request.Weight
	return s.Store.Join(ctx, request)
}

type drawEligibilityData struct {
	followCalls int
}

func (*drawEligibilityData) Snapshot(context.Context, uint64, uint64) (loyaltyrpc.BalanceSnapshot, error) {
	return loyaltyrpc.BalanceSnapshot{Points: 40, Found: true}, nil
}

func (d *drawEligibilityData) FollowedAt(context.Context, uint64, uint64) (time.Time, bool, error) {
	d.followCalls++
	return time.Time{}, false, nil
}

type drawEligibilityCase struct {
	name    string
	rules   eligibility.Rules
	config  codec.RawMessage
	calls   []drawChatCall
	results drawEligibilityResult
}

type drawChatCall struct {
	command string
	viewer  string
	badge   string
}

type drawEligibilityResult struct {
	entries, refused, follows int
	replies                   []string
	weight                    int64
}

func TestDrawEligibilityProcess(t *testing.T) {
	rejoin := []drawChatCall{{command: "join", viewer: "7"}, {command: "join", viewer: "7"}}
	for _, tc := range []drawEligibilityCase{
		{name: "refused once", rules: eligibility.Rules{Role: eligibility.Sub}, calls: rejoin,
			results: drawEligibilityResult{refused: 1, replies: []string{"@viewer your chat role does not meet the entry requirement."}}},
		{name: "refusals can be silent", rules: eligibility.Rules{Role: eligibility.Sub}, config: codec.RawMessage(`{"refusalLineOn":false}`),
			calls: rejoin, results: drawEligibilityResult{refused: 1}},
		{name: "own bot messages are ignored", calls: []drawChatCall{{command: "join", viewer: "99"}}},
		{name: "weighted join skips draw-time lookups", rules: eligibility.Rules{SubLuckPercent: 200, FollowersOnly: true},
			calls: []drawChatCall{{"join", "7", "subscriber"}}, results: drawEligibilityResult{entries: 1, weight: 200}},
		{name: "check reports standings with timestamp", rules: eligibility.Rules{MinPoints: 100, FollowersOnly: true},
			calls: []drawChatCall{{command: "check", viewer: "7"}}, results: drawEligibilityResult{
				replies: []string{"@viewer you need 100 points (you had 40 at 20:14 UTC). Checked at the draw: channel follow."}}},
		{name: "check uses a per-viewer cooldown and checks follow", rules: eligibility.Rules{FollowersOnly: true},
			calls:   []drawChatCall{{command: "check", viewer: "7"}, {command: "check", viewer: "7"}, {command: "check", viewer: "8"}},
			results: drawEligibilityResult{follows: 2, replies: []string{"@viewer a channel follow could not be verified.", "@viewer a channel follow could not be verified."}}},
		{name: "check confirms eligibility without entering", calls: []drawChatCall{{command: "check", viewer: "7"}},
			results: drawEligibilityResult{replies: []string{"@viewer you meet the entry requirements."}}},
	} {
		t.Run(tc.name, tc.run)
	}
}

func (tc drawEligibilityCase) run(t *testing.T) {
	vc := valkeytest.Real(t)
	data := &drawEligibilityData{}
	checker := eligibility.NewChecker(eligibility.Ports{Standings: data, Follows: data, Now: func() time.Time {
		return time.Date(2026, 11, 19, 20, 14, 0, 0, time.UTC)
	}})
	t.Cleanup(checker.Close)
	store := &observedDrawStore{Store: draw.NewStore(vc, draw.Config{Verifier: checker, Payer: drawTestPayer{},
		Announcer: drawTestAnnouncer{}, Winners: draw.DiscardWinnerRecords{}})}
	pub := &variablePublisher{}
	deps := engine.Deps{Draw: store, Eligibility: checker, Pub: pub,
		Proj: &fakeProj{modules: []projection.ModuleView{{Name: "giveaway", Configs: tc.config}}},
		Live: &fakeLive{live: true}, Greet: &fakeGreet{}, Cooldown: engine.NewValkeyCooldown(vc),
		Special: engine.NewSpecialSet(""), Log: zap.NewNop()}
	p := engine.NewPipeline(deps, engine.NewRegistry(deps.Log, Draw(deps)), engine.Config{OutgressStandard: "outgress.standard", BotID: "99"})
	t.Cleanup(p.Close)
	round := tc.open(t, store.Store)
	for _, call := range tc.calls {
		call.process(t, p, round.Channel)
	}
	rounds, err := store.Live(t.Context(), round.Channel)
	require.NoError(t, err)
	require.Len(t, rounds, 1)
	tc.assertResult(t, drawEligibilityObservation{rounds[0], data.followCalls, pub, store.weight})
}

func (tc drawEligibilityCase) open(t *testing.T, store *draw.Store) draw.Round {
	t.Helper()
	raw, err := json.Marshal(tc.rules)
	require.NoError(t, err)
	channel := draw.ChannelID(time.Now().UnixNano())
	round, status, err := store.Open(t.Context(), draw.OpenRequest{
		Channel: channel, Terms: draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "Prize"},
			Rules: raw, ClosesAt: time.Now().Add(time.Minute)},
	})
	require.NoError(t, err)
	require.Equal(t, draw.Opened, status)
	t.Cleanup(func() {
		_, err := store.Cancel(context.Background(), draw.ModAction{Channel: channel, Round: round.ID})
		require.NoError(t, err)
	})
	return round
}

func (c drawChatCall) process(t *testing.T, pipe *engine.Pipeline, channel draw.ChannelID) {
	t.Helper()
	event := lane.Envelope{Type: "channel.chat.message", Lane: "standard", BroadcasterUserID: strconv.FormatUint(uint64(channel), 10),
		BroadcasterUserLogin: "streamer", ChatterUserID: c.viewer, ChatterUserLogin: "viewer", Text: "!" + c.command}
	if c.badge != "" {
		event.Badges = []lane.Badge{{SetID: c.badge}}
	}
	body, err := codec.Marshal(event)
	require.NoError(t, err)
	require.NoError(t, pipe.Process(bus.NewMessage("draw-eligibility", body)))
}

type drawEligibilityObservation struct {
	round   draw.RoundView
	follows int
	pub     *variablePublisher
	weight  int64
}

func (tc drawEligibilityCase) assertResult(t *testing.T, observed drawEligibilityObservation) {
	t.Helper()
	var replies []string
	for _, m := range observed.pub.messages {
		var payload struct{ Message string }
		require.NoError(t, codec.Unmarshal(m.Payload, &payload))
		replies = append(replies, payload.Message)
	}
	require.Equal(t, []int{tc.results.entries, tc.results.refused, tc.results.follows},
		[]int{observed.round.Entries, int(observed.round.Refused), observed.follows})
	require.Equal(t, tc.results.replies, replies)
	require.Equal(t, tc.results.weight, observed.weight)
}
