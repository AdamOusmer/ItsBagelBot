// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type drawTriggerStore struct {
	fakeDraw
}

func (d *drawTriggerStore) Settling(context.Context, draw.ChannelID, draw.Kind) (draw.Round, []draw.Slot, bool, error) {
	d.lookups++
	if len(d.slots) == 0 {
		return draw.Round{}, nil, false, nil
	}
	kind := d.kind
	if kind == "" {
		kind = draw.KindRaffle
	}
	return draw.Round{ID: "settling", Terms: draw.Terms{Kind: kind}}, d.slots, true, nil
}

type drawTriggerCase struct {
	name    string
	command string
	draw    fakeDraw
	views   []projection.ModuleView
	history bool
	want    drawChatResult
}

var (
	raffleNoTicketView   = enabledVariables(raffleModuleName, `{"disabledTriggers":"ticket"}`)
	raffleNoJoinView     = enabledVariables(raffleModuleName, `{"disabledTriggers":"join"}`)
	raffleNoWinnerView   = enabledVariables(raffleModuleName, `{"disabledTriggers":"winner"}`)
	queueEnabledView     = enabledVariables(queueModuleName, "")
	giveawayNoClaimView  = enabledVariables(engine.GiveawayModuleName, `{"disabledTriggers":"claim"}`)
	giveawayNoCheckView  = enabledVariables(engine.GiveawayModuleName, `{"disabledTriggers":"check"}`)
	giveawayNoWinnerView = enabledVariables(engine.GiveawayModuleName, `{"disabledTriggers":"winner"}`)
	expectedDrawJoin     = draw.JoinRequest{Channel: 2, Round: "active", Viewer: "9", Login: "viewer", Weight: draw.Pins.EntryWeight}

	drawTriggerCases = []drawTriggerCase{
		{
			name:    "disabling ticket on raffle yields ticket to custom",
			command: "ticket",
			draw:    fakeDraw{joinable: true, kind: draw.KindRaffle},
			views:   []projection.ModuleView{raffleNoTicketView},
			want:    drawChatResult{message: "custom ticket"},
		},
		{
			name:    "disabling ticket on raffle keeps enter working",
			command: "enter",
			draw:    fakeDraw{joinable: true, kind: draw.KindRaffle},
			views:   []projection.ModuleView{raffleNoTicketView},
			want:    drawChatResult{joined: expectedDrawJoin, message: "Entry is free."},
		},
		{
			name:    "disabling ticket on raffle keeps join working",
			command: "join",
			draw:    fakeDraw{joinable: true, kind: draw.KindRaffle},
			views:   []projection.ModuleView{raffleNoTicketView},
			want:    drawChatResult{joined: expectedDrawJoin},
		},
		{
			name:    "disabling join on raffle yields open raffle join to queue",
			command: "join",
			draw:    fakeDraw{joinable: true, kind: draw.KindRaffle},
			views:   []projection.ModuleView{queueEnabledView, raffleNoJoinView},
			want:    drawChatResult{message: "@viewer you joined the queue at position #1."},
		},
		{
			name:    "disabling join on raffle yields open raffle join to custom when queue is off",
			command: "join",
			draw:    fakeDraw{joinable: true, kind: draw.KindRaffle},
			views:   []projection.ModuleView{raffleNoJoinView},
			want:    drawChatResult{message: "custom join"},
		},
		{
			name:    "disabling join on raffle suppresses closed raffle reply",
			command: "join",
			views:   []projection.ModuleView{raffleNoJoinView},
			want:    drawChatResult{message: "custom join"},
		},
		{
			name:    "disabling claim on giveaway yields to custom",
			command: "claim",
			draw:    fakeDraw{claims: draw.ClaimOutcome{Claimed: 1}},
			views:   []projection.ModuleView{giveawayNoClaimView},
			want:    drawChatResult{message: "custom claim"},
		},
		{
			name:    "disabling check on giveaway yields to custom",
			command: "check",
			draw:    fakeDraw{joinable: true, kind: draw.KindGiveaway},
			views:   []projection.ModuleView{giveawayNoCheckView},
			want:    drawChatResult{message: "custom check"},
		},
		{
			name:    "disabling winner on giveaway yields giveaway history to custom",
			command: "winner",
			views:   []projection.ModuleView{giveawayNoWinnerView},
			history: true,
			want:    drawChatResult{message: "custom winner"},
		},
		{
			name:    "disabling winner on giveaway yields settling giveaway to custom",
			command: "winner",
			draw:    fakeDraw{kind: draw.KindGiveaway, slots: []draw.Slot{{Login: "win", State: draw.SlotWon}}},
			views:   []projection.ModuleView{giveawayNoWinnerView},
			history: true,
			want:    drawChatResult{message: "custom winner"},
		},
		{
			name:    "disabling winner on raffle yields settling raffle to custom",
			command: "winner",
			draw:    fakeDraw{kind: draw.KindRaffle, slots: []draw.Slot{{Login: "win", State: draw.SlotUnpaid}}},
			views:   []projection.ModuleView{raffleNoWinnerView},
			want:    drawChatResult{message: "custom winner"},
		},
	}
)

func TestDrawModuleTriggerToggles(t *testing.T) {
	for _, tc := range drawTriggerCases {
		t.Run(tc.name, tc.run)
	}
}

func (tc drawTriggerCase) run(t *testing.T) {
	pub := &variablePublisher{}
	store := &drawTriggerStore{fakeDraw: tc.draw}
	checker := eligibility.NewChecker(eligibility.Ports{})
	t.Cleanup(checker.Close)
	deps := engine.Deps{
		Proj: &fakeProj{modules: tc.views, commands: map[string]projection.Command{
			tc.command: {Name: tc.command, IsActive: true, Response: "custom " + tc.command, Perm: "everyone"},
		}},
		Draw:            store,
		Eligibility:     checker,
		Queue:           &fakeQueue{open: true},
		Pub:             pub,
		GiveawayWinners: giveawayChatWinners{empty: !tc.history},
		Live:            &fakeLive{live: true},
		Greet:           &fakeGreet{},
		Cooldown:        engine.NoopCooldown{},
		Special:         engine.NewSpecialSet(""),
		Log:             zap.NewNop(),
	}
	p := engine.NewPipeline(deps, engine.NewRegistry(deps.Log, All(deps)...), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(p.Close)

	call := drawChatCall{command: tc.command, viewer: "9"}
	require.NoError(t, call.process(t, p, 2))
	got := drawChatResult{joined: store.joined, claimed: store.claimed, message: firstDrawReply(t, pub)}
	require.Equal(t, tc.want, got)
}

func firstDrawReply(t *testing.T, pub *variablePublisher) string {
	t.Helper()
	replies := drawReplies(t, pub)
	if len(replies) == 0 {
		return ""
	}
	return replies[0]
}
