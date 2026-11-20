// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type giveawayChatDraw struct {
	engine.DrawStore
	busy      bool
	open      draw.OpenRequest
	closed    draw.CloseRequest
	cancelled draw.ModAction
	rerolled  draw.SlotAction
}

func (d *giveawayChatDraw) Joinable(context.Context, draw.ChannelID) (draw.Round, bool, error) {
	return draw.Round{Terms: draw.Terms{Kind: draw.KindGiveaway}}, d.busy, nil
}

func (d *giveawayChatDraw) Open(_ context.Context, req draw.OpenRequest) (draw.Round, draw.OpenOutcome, error) {
	d.open = req
	at := time.Now()
	r := draw.Round{ID: req.ID, Channel: req.Channel, Terms: req.Terms, OpenedAt: at, ClosesAt: at.Add(req.Duration)}
	return r, draw.Opened, nil
}

func (d *giveawayChatDraw) Close(_ context.Context, req draw.CloseRequest) (bool, error) {
	d.closed = req
	return true, nil
}

func (d *giveawayChatDraw) Cancel(_ context.Context, req draw.ModAction) (bool, error) {
	d.cancelled = req
	return true, nil
}

func (d *giveawayChatDraw) Reroll(_ context.Context, req draw.SlotAction) (int, error) {
	d.rerolled = req
	return 1, nil
}

type giveawayChatWatch struct {
	engine.LoyaltyTicker
}

func (giveawayChatWatch) AccrualHealth(context.Context, uint64) engine.AccrualState {
	return engine.AccrualStalled
}

type giveawayChatCase struct {
	name, command, reply string
	viewer, busy         bool
	prize, action        string
	slot                 int
}

func TestGiveawayProcess(t *testing.T) {
	for _, tc := range []giveawayChatCase{
		{name: "mod starts with saved preset", command: "start REGULARS Steam Deck", prize: "Steam Deck", reply: "7 days of following"},
		{name: "unknown word stays in prize", command: "start Steam Deck", prize: "Steam Deck", reply: "2 winners"},
		{name: "defaults", command: "start", prize: "Mystery box", reply: "Mystery box"},
		{name: "blocked prize", command: "start " + blockedGiveawayPrize, reply: "Choose a different giveaway prize name."},
		{name: "stalled accrual", command: "start points Game", reply: "Watch time accrual has stalled."},
		{name: "busy", command: "start Game", busy: true, reply: "A giveaway is already open."},
		{name: "viewer cannot start", command: "start Game", viewer: true},
		{name: "draw", command: "draw", action: "close"},
		{name: "cancel", command: "cancel", action: "cancel", reply: "The giveaway was cancelled."},
		{name: "reroll all", command: "!reroll", action: "reroll"},
		{name: "reroll slot", command: "!reroll 2", action: "reroll", slot: 2},
		{name: "viewer cannot reroll", command: "!reroll 2", viewer: true},
		{name: "invalid slot", command: "!reroll 0", reply: "Use !reroll"},
	} {
		t.Run(tc.name, tc.run)
	}
}

const blockedGiveawayPrize = "blocked prize"

func (tc giveawayChatCase) run(t *testing.T) {
	store := &giveawayChatDraw{busy: tc.busy}
	pub := &variablePublisher{}
	proj := &fakeProj{modules: []projection.ModuleView{
		enabledVariables(engine.LoyaltyModuleName, ""),
		enabledVariables(engine.GiveawayModuleName, `{"defaultPrize":"Mystery box","winners":2,"claimSeconds":0,
   "openedMessage":"{giveaway:prize}, {giveaway:count} winners. {giveaway:requirements}",
   "presets":[{"name":"regulars","rules":{"min_follow_days":7}},{"name":"points","rules":{"min_points":100}}]}`),
	}}
	log := zap.NewNop()
	deps := engine.Deps{Draw: store, Pub: pub, Proj: proj, Log: log, LoyaltyTick: giveawayChatWatch{}, Automod: giveawayTestGate(t)}
	pipe := engine.NewPipeline(deps, engine.NewRegistry(log, Giveaway(deps)), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(pipe.Close)
	command := tc.command
	if !strings.HasPrefix(command, "!") {
		command = "!giveaway " + command
	}
	env := chatCtx("42", "viewer", tc.badge()).Env
	env.BroadcasterUserID, env.Lane, env.Text = "2", "standard", command
	body, err := codec.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, pipe.Process(bus.NewMessage("test", body)))
	tc.check(t, store, strings.Join(drawReplies(t, pub), " "))
}

func (tc giveawayChatCase) badge() string {
	if tc.viewer {
		return ""
	}
	return "moderator"
}

func (tc giveawayChatCase) check(t *testing.T, store *giveawayChatDraw, reply string) {
	t.Helper()
	require.Equal(t, tc.prize, store.open.Terms.Prize.Item)
	switch tc.action {
	case "close":
		require.Equal(t, draw.CloseRequest{Channel: 2, Actor: "42", Kind: draw.KindGiveaway}, store.closed)
	case "cancel":
		require.Equal(t, draw.ModAction{Channel: 2, Actor: "42", Kind: draw.KindGiveaway}, store.cancelled)
	case "reroll":
		require.Equal(t, draw.SlotAction{Channel: 2, Actor: "42", Kind: draw.KindGiveaway, Slot: tc.slot}, store.rerolled)
	default:
		require.Equal(t, draw.SlotAction{}, store.rerolled)
	}
	if tc.reply == "" {
		require.Empty(t, reply)
		return
	}
	require.Contains(t, reply, tc.reply)
}

func giveawayTestGate(t *testing.T) *automod.Gate {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hate.txt"), []byte(blockedGiveawayPrize+"\n"), 0600))
	lexicon, err := automod.LoadLexiconDir(dir)
	require.NoError(t, err)
	gate := automod.New()
	gate.SetLexicon(lexicon)
	return gate
}

type giveawayVariableDraw struct {
	engine.DrawStore
	round draw.RoundView
}

func (d giveawayVariableDraw) Live(context.Context, draw.ChannelID) ([]draw.RoundView, error) {
	return []draw.RoundView{d.round}, nil
}

func TestGiveawayRecipeTokensProcess(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase draw.Phase
		want  string
	}{
		{"active", draw.PhaseOpen, `^Game 6 [0-9]+$`},
		{"settling", draw.PhaseSettling, `^closed 0 0$`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			views := []projection.ModuleView{enabledVariables(engine.GiveawayModuleName, "")}
			response := "{giveaway:prize|closed} {giveaway:count|0} {giveaway:left|0}"
			got := runVariableCommand(t, response, views, &fakeGossip{}, func(d *engine.Deps) {
				d.Draw = giveawayVariableDraw{round: draw.RoundView{Entries: 6, Round: draw.Round{
					Phase: tc.phase, ClosesAt: time.Now().Add(time.Minute), Terms: draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "Game"}},
				}}}
			})
			require.Regexp(t, tc.want, got)
		})
	}
}

func TestGiveawayAnnouncements(t *testing.T) {
	for _, tc := range []struct {
		name   string
		event  draw.NoticeEvent
		claim  time.Duration
		config string
		want   []string
	}{
		{"win with a claim", draw.NoticeWinners, time.Minute, "", []string{"@viewer won Game! Type !claim within 1m."}},
		{"win without a claim", draw.NoticeWinners, 0, "", []string{"@viewer won Game!"}},
		{"custom win", draw.NoticeWinners, 0, `{"wonMessage":"{giveaway:targets} gets {giveaway:prize}."}`, []string{"@viewer gets Game."}},
		{"win disabled", draw.NoticeWinners, 0, `{"wonEnabled":false}`, nil},
		{"no-show", draw.NoticeNoShow, time.Minute, "", []string{"@viewer missed the claim deadline."}},
		{"no-show disabled", draw.NoticeNoShow, time.Minute, `{"noShowEnabled":false}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub := &variablePublisher{}
			proj := &fakeProj{modules: []projection.ModuleView{enabledVariables(engine.GiveawayModuleName, tc.config)}}
			announcer := engine.NewDrawAnnouncer(engine.AnnouncementConfig{Proj: proj, Pub: pub}, zap.NewNop(), nil)
			round := draw.Round{Channel: 2, Terms: draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "Game"}, ClaimWindow: tc.claim}}
			announcer.Announce(t.Context(), draw.Notice{Round: round, Event: tc.event, Slots: []draw.Slot{{Login: "viewer"}}})
			require.Equal(t, tc.want, drawReplies(t, pub))
		})
	}
}
