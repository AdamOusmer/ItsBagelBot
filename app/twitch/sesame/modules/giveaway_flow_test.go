// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/event/lane"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type giveawayFlowDraw struct {
	engine.DrawStore
	view    draw.RoundView
	changed int
	target  draw.SlotAction
}

func (d *giveawayFlowDraw) Joinable(context.Context, draw.ChannelID) (draw.Round, bool, error) {
	return d.view.Round, d.view.Phase == draw.PhaseOpen, nil
}

func (d *giveawayFlowDraw) Live(context.Context, draw.ChannelID) ([]draw.RoundView, error) {
	return []draw.RoundView{d.view}, nil
}

func (*giveawayFlowDraw) Close(context.Context, draw.CloseRequest) (bool, error) {
	return false, nil
}

func (d *giveawayFlowDraw) Reroll(_ context.Context, action draw.SlotAction) (int, error) {
	d.target = action
	return d.changed, nil
}

type giveawayChatWinners struct {
	engine.GiveawayWinnersStore
	empty bool
}

func (s giveawayChatWinners) Recent(context.Context, uint64, modulesrpc.GiveawayRecentQuery) ([]modulesrpc.GiveawayWinner, error) {
	if s.empty {
		return nil, nil
	}
	return []modulesrpc.GiveawayWinner{{Login: "winner", Prize: "Game", RoundID: "prior"}}, nil
}

type giveawayFlowCase struct {
	name, command, reply string
	view                 draw.Phase
	slots                []draw.Slot
	viewer               bool
	changed              int
	login                string
}

func TestGiveawayChatFlow(t *testing.T) {
	now := time.Now()
	for _, tc := range []giveawayFlowCase{
		{name: "viewer sees open giveaway", command: "giveaway", view: draw.PhaseOpen, viewer: true, reply: "Giveaway for Game."},
		{name: "viewer sees last winners", command: "giveaway", viewer: true, reply: "Last giveaway winners for Game: @winner."},
		{name: "viewer cannot draw", command: "giveaway draw", view: draw.PhaseOpen, viewer: true},
		{name: "draw already settling", command: "giveaway draw", view: draw.PhaseSettling, reply: "Use End unfinished on the dashboard."},
		{name: "no draw open", command: "giveaway draw", reply: "There is no giveaway open"},
		{name: "nothing to reroll", command: "reroll 2", view: draw.PhaseSettling, reply: "There are no winners to reroll"},
		{name: "claim window is still open", command: "reroll @WINNER", view: draw.PhaseSettling, login: "winner",
			slots: []draw.Slot{{N: 1, State: draw.SlotWon, Login: "winner", Deadline: now.Add(40 * time.Second)}},
			reply: "No-shows can be rerolled when the claim window ends in"},
		{name: "login rerolls a current winner", command: "reroll @WiNnEr", view: draw.PhaseSettling, changed: 1, login: "winner"},
	} {
		t.Run(tc.name, tc.run)
	}
}

func (tc giveawayFlowCase) run(t *testing.T) {
	store := &giveawayFlowDraw{changed: tc.changed, view: draw.RoundView{Round: draw.Round{
		ID: "active", Phase: tc.view, ClosesAt: time.Now().Add(time.Minute),
		Terms: draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "Game"}},
	}, Slots: tc.slots}}
	p, pub := giveawayFlowPipeline(t, store, engine.NoopCooldown{})
	call := drawChatCall{command: tc.command, viewer: "7"}
	if !tc.viewer {
		call.badges = []lane.Badge{{SetID: "moderator"}}
	}
	require.NoError(t, call.process(t, p, 42))
	require.Equal(t, tc.login, store.target.Login)
	tc.checkReplies(t, drawReplies(t, pub))
}

func giveawayFlowPipeline(t *testing.T, store engine.DrawStore, cooldown engine.CooldownStore) (*engine.Pipeline, *variablePublisher) {
	t.Helper()
	pub := &variablePublisher{}
	deps := engine.Deps{Draw: store, Log: zap.NewNop(), Pub: pub,
		Proj:            &fakeProj{modules: []projection.ModuleView{enabledVariables(engine.GiveawayModuleName, "")}},
		GiveawayWinners: giveawayChatWinners{}, Cooldown: cooldown}
	p := engine.NewPipeline(deps, engine.NewRegistry(deps.Log, Giveaway(deps)), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(p.Close)
	return p, pub
}

func TestGiveawayViewerStatusCooldown(t *testing.T) {
	cooldown := &fakeCooldown{allow: []bool{true, false, true}}
	p, pub := giveawayFlowPipeline(t, &giveawayFlowDraw{}, cooldown)
	for _, call := range []drawChatCall{
		{command: "giveaway", viewer: "7"},
		{command: "giveaway", viewer: "7"},
		{command: "giveaway", viewer: "8"},
		{command: "giveaway status", viewer: "7", badges: []lane.Badge{{SetID: "moderator"}}},
	} {
		require.NoError(t, call.process(t, p, 42))
	}
	require.Len(t, drawReplies(t, pub), 3)
	require.Equal(t, []string{"cooldown:cmd:42:giveaway:viewer:7", "cooldown:cmd:42:giveaway:viewer:7",
		"cooldown:cmd:42:giveaway:viewer:8"}, cooldown.keys)
	require.Equal(t, []time.Duration{draw.Pins.GiveawayStatusCooldown, draw.Pins.GiveawayStatusCooldown, draw.Pins.GiveawayStatusCooldown}, cooldown.ttls)
}

func (tc giveawayFlowCase) checkReplies(t *testing.T, got []string) {
	t.Helper()
	if tc.reply == "" {
		require.Empty(t, got)
		return
	}
	require.Len(t, got, 1)
	require.Contains(t, got[0], tc.reply)
}
