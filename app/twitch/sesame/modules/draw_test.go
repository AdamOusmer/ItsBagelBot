// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeDraw struct {
	engine.DrawStore
	joinable bool
	phase    draw.Phase
	kind     draw.Kind
	closed   bool
	lookups  int
	joined   draw.JoinRequest
	refusals int
	batched  int
	claims   draw.ClaimOutcome
	claimed  draw.ViewerID
	slots    []draw.Slot
}

func (d *fakeDraw) Joinable(ctx context.Context, channel draw.ChannelID) (draw.Round, bool, error) {
	if d.DrawStore != nil {
		return d.DrawStore.Joinable(ctx, channel)
	}
	d.lookups++
	if d.joinable || d.phase != "" {
		return draw.Round{ID: "active", Phase: d.phase, Terms: draw.Terms{Kind: d.kind}}, d.joinable, nil
	}
	return draw.Round{}, false, nil
}

func (d *fakeDraw) Join(ctx context.Context, req draw.JoinRequest) (draw.JoinOutcome, error) {
	added := !d.closed && d.joined != req
	d.joined = req
	if d.DrawStore != nil {
		return d.DrawStore.Join(ctx, req)
	}
	return draw.JoinOutcome{Open: !d.closed, Added: added, Count: 1}, nil
}

func (d *fakeDraw) Refuse(ctx context.Context, req draw.Refusal) (bool, int64, error) {
	d.refusals++
	first, count, err := d.DrawStore.Refuse(ctx, req)
	if first && req.Notice != nil {
		d.batched++
	}
	return first, count, err
}

func (d *fakeDraw) LookupClaims(_ context.Context, _ draw.ChannelID, viewer draw.ViewerID) (draw.ClaimLookup, error) {
	d.lookups++
	if d.claims != (draw.ClaimOutcome{}) {
		d.claimed = viewer
	}
	return draw.ClaimLookup{Found: d.claims != (draw.ClaimOutcome{})}, nil
}

func (d *fakeDraw) ClaimResolved(_ context.Context, _ draw.ClaimLookup) (draw.ClaimOutcome, error) {
	return d.claims, nil
}

func (d *fakeDraw) Settling(context.Context, draw.ChannelID, draw.Kind) (draw.Round, []draw.Slot, bool, error) {
	d.lookups++
	if len(d.slots) == 0 {
		return draw.Round{}, nil, false, nil
	}
	return draw.Round{ID: "settling", Terms: draw.Terms{Kind: draw.KindRaffle}}, d.slots, true, nil
}

type drawChatResult struct {
	message string
	joined  draw.JoinRequest
	claimed draw.ViewerID
}

type drawChatCase struct {
	name, command string
	draw          fakeDraw
	views         []projection.ModuleView
	history       bool
	want          drawChatResult
}

func TestDrawSharedCommandsProcess(t *testing.T) {
	raffle := enabledVariables(raffleModuleName, "")
	queue := []projection.ModuleView{enabledVariables(queueModuleName, ""), raffle}
	giveaway := []projection.ModuleView{enabledVariables(engine.GiveawayModuleName, "")}
	join := draw.JoinRequest{Channel: 2, Round: "active", Viewer: "9", Login: "viewer", Weight: draw.Pins.EntryWeight}
	cases := []drawChatCase{
		{name: "open raffle takes join before queue", command: "join", draw: fakeDraw{joinable: true, kind: draw.KindRaffle},
			views: queue, want: drawChatResult{joined: join}},
		{name: "enter aliases join while open", command: "enter", draw: fakeDraw{joinable: true, kind: draw.KindRaffle},
			views: queue, want: drawChatResult{joined: join, message: "Entry is free."}},
		{name: "ticket aliases join while open", command: "ticket", draw: fakeDraw{joinable: true, kind: draw.KindGiveaway},
			views: queue, want: drawChatResult{joined: join, message: "Entry is free."}},
		{name: "giveaway closes during alias admission", command: "enter", draw: fakeDraw{joinable: true, kind: draw.KindGiveaway, closed: true},
			views: queue, want: drawChatResult{joined: join, message: "The draw has closed."}},
		{name: "raffle closes during admission", command: "join", draw: fakeDraw{joinable: true, kind: draw.KindRaffle, closed: true},
			views: []projection.ModuleView{{Name: raffleModuleName, Configs: codec.RawMessage(`{"noRaffleMessage":"Raffle just closed."}`)}},
			want:  drawChatResult{joined: join, message: "Raffle just closed."}},
		{name: "round past its deadline claims join before the sweep", command: "join", draw: fakeDraw{phase: draw.PhaseOpen}, views: queue,
			want: drawChatResult{message: "The draw has closed."}},
		{name: "closing claims join before queue", command: "join", draw: fakeDraw{phase: draw.PhaseClosing}, views: queue,
			want: drawChatResult{message: "The draw has closed."}},
		{name: "settling claims join before queue", command: "join", draw: fakeDraw{phase: draw.PhaseSettling}, views: queue,
			want: drawChatResult{message: "The draw has closed."}},
		{name: "raffle closed reply with no round", command: "join", views: []projection.ModuleView{raffle},
			want: drawChatResult{message: "no raffle is running right now."}},
		{name: "queue receives unclaimed join", command: "join", views: queue,
			want: drawChatResult{message: "you joined the queue"}},
		{name: "claim works with opening modules off", command: "claim", draw: fakeDraw{claims: draw.ClaimOutcome{Claimed: 1}},
			want: drawChatResult{message: "@viewer your prize is confirmed!", claimed: "9"}},
		{name: "winner shows unpaid slots before giveaway history", command: "winner", views: giveaway, history: true,
			draw: fakeDraw{slots: []draw.Slot{{Login: "new", State: draw.SlotUnpaid}}},
			want: drawChatResult{message: "Could not pay: @new."}},
		{name: "winner shows giveaway history", command: "winner", views: giveaway, history: true,
			want: drawChatResult{message: "Last giveaway winners for Game: @winner."}},
		{name: "winner replies when giveaway history is empty", command: "winner", views: giveaway,
			want: drawChatResult{message: "No winners are available yet."}},
		{name: "winner yields with raffle on and no settling round", command: "winner", views: []projection.ModuleView{raffle},
			want: drawChatResult{message: "custom winner"}},
	}
	for _, command := range []string{"join", "enter", "ticket", "check", "claim", "winner"} {
		cases = append(cases, drawChatCase{name: "custom " + command + " runs after yield", command: command,
			want: drawChatResult{message: "custom " + command}})
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc drawChatCase) run(t *testing.T) {
	pub := &variablePublisher{}
	checker := eligibility.NewChecker(eligibility.Ports{})
	t.Cleanup(checker.Close)
	deps := engine.Deps{
		Proj: &fakeProj{modules: tc.views, commands: map[string]projection.Command{
			tc.command: {Name: tc.command, IsActive: true, Response: "custom " + tc.command, Perm: "everyone"},
		}},
		Draw: &tc.draw, Eligibility: checker, Queue: &fakeQueue{open: true}, Pub: pub,
		GiveawayWinners: giveawayChatWinners{empty: !tc.history},
		Live:            &fakeLive{live: true}, Greet: &fakeGreet{}, Cooldown: engine.NoopCooldown{},
		Special: engine.NewSpecialSet(""), Log: zap.NewNop(),
	}
	p := engine.NewPipeline(deps, engine.NewRegistry(deps.Log, All(deps)...), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(p.Close)
	call := drawChatCall{command: tc.command, viewer: "9"}
	require.NoError(t, call.process(t, p, 2))
	require.Equal(t, 1, tc.draw.lookups)
	tc.check(t, pub)
}

type drawChatCall struct {
	command string
	viewer  draw.ViewerID
	badges  []lane.Badge
}

func (c drawChatCall) process(t *testing.T, pipe *engine.Pipeline, channel draw.ChannelID) error {
	t.Helper()
	broadcaster := strconv.FormatUint(uint64(channel), 10)
	viewer := string(c.viewer)
	if viewer == "" {
		viewer = broadcaster
	}
	event := lane.Envelope{Type: "channel.chat.message", Lane: "standard", BroadcasterUserID: broadcaster,
		BroadcasterUserLogin: "streamer", ChatterUserID: viewer, ChatterUserLogin: "viewer", Text: "!" + c.command, Badges: c.badges}
	body, err := codec.Marshal(event)
	require.NoError(t, err)
	return pipe.Process(bus.NewMessage("draw-command", body))
}

func (tc drawChatCase) check(t *testing.T, pub *variablePublisher) {
	t.Helper()
	got := drawChatResult{joined: tc.draw.joined, claimed: tc.draw.claimed, message: tc.want.message}
	require.Equal(t, tc.want, got)
	replies := drawReplies(t, pub)
	if tc.want.message == "" {
		require.Empty(t, replies)
		return
	}
	require.Len(t, replies, 1)
	require.Contains(t, replies[0], tc.want.message)
}

func drawReplies(t *testing.T, pub *variablePublisher) []string {
	t.Helper()
	var replies []string
	for _, m := range pub.messages {
		var payload struct{ Message string }
		require.NoError(t, codec.Unmarshal(m.Payload, &payload))
		replies = append(replies, payload.Message)
	}
	return replies
}
