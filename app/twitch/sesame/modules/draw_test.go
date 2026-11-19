// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
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
	joined   draw.JoinRequest
	claims   draw.ClaimOutcome
	claimed  draw.ViewerID
	slots    []draw.Slot
}

func (d *fakeDraw) Joinable(context.Context, draw.ChannelID) (draw.Round, bool, error) {
	return draw.Round{ID: "active"}, d.joinable, nil
}

func (d *fakeDraw) Join(_ context.Context, req draw.JoinRequest) (draw.JoinOutcome, error) {
	d.joined = req
	return draw.JoinOutcome{Open: true, Added: true}, nil
}

func (d *fakeDraw) CanClaim(context.Context, draw.ChannelID, draw.ViewerID) (bool, error) {
	return d.claims != (draw.ClaimOutcome{}), nil
}

func (d *fakeDraw) Claim(_ context.Context, _ draw.ChannelID, viewer draw.ViewerID) (draw.ClaimOutcome, error) {
	d.claimed = viewer
	return d.claims, nil
}

func (d *fakeDraw) Settling(context.Context, draw.ChannelID, draw.Kind) (draw.Round, []draw.Slot, bool, error) {
	return draw.Round{Terms: draw.Terms{Kind: draw.KindRaffle}}, d.slots, len(d.slots) != 0, nil
}

type drawChatResult struct {
	message, legacyClaim string
	legacyJoined         []string
	joined               draw.JoinRequest
	claimed              draw.ViewerID
}

type drawChatCase struct {
	name, command string
	draw          fakeDraw
	raffle        fakeRaffle
	views         []projection.ModuleView
	want          drawChatResult
}

func TestDrawSharedCommandsProcess(t *testing.T) {
	legacy := fakeRaffle{open: true, lastFound: true, lastResult: &engine.RaffleResult{Winners: winners("legacy"), Entrants: 2}}
	queue := []projection.ModuleView{enabledVariables(queueModuleName, "")}
	cases := []drawChatCase{
		{name: "new round beats legacy and queue", command: "join", draw: fakeDraw{joinable: true}, raffle: legacy, views: queue,
			want: drawChatResult{joined: draw.JoinRequest{Channel: 2, Round: "active", Viewer: "9", Login: "viewer", Weight: 100}}},
		{name: "legacy open beats queue even with raffle disabled", command: "join", raffle: legacy, views: queue,
			want: drawChatResult{legacyJoined: []string{"9"}}},
		{name: "queue receives unclaimed join", command: "join", views: queue,
			want: drawChatResult{message: "you joined the queue"}},
		{name: "new claim beats legacy with opening modules off", command: "claim", draw: fakeDraw{claims: draw.ClaimOutcome{Claimed: 1}}, raffle: legacy,
			want: drawChatResult{message: "@viewer your prize is confirmed!", claimed: "9"}},
		{name: "legacy claim remains available with raffle disabled", command: "claim",
			raffle: fakeRaffle{lastFound: true, lastResult: legacy.lastResult, claim: engine.ClaimOk},
			views:  []projection.ModuleView{{Name: raffleModuleName, Configs: []byte(raffleClaimOn)}},
			want:   drawChatResult{message: "your prize is confirmed", legacyClaim: "9", claimed: "9"}},
		{name: "new winner shows unpaid slots before legacy results", command: "winner", raffle: legacy,
			draw: fakeDraw{slots: []draw.Slot{{Login: "new", State: draw.SlotUnpaid}}},
			want: drawChatResult{message: "Could not pay: @new."}},
		{name: "legacy winner works with raffle disabled", command: "winner", raffle: legacy,
			want: drawChatResult{message: "Last raffle: @legacy"}},
	}
	for _, command := range []string{"join", "claim", "winner"} {
		cases = append(cases, drawChatCase{name: "custom " + command + " runs after yield", command: command,
			want: drawChatResult{message: "custom " + command}})
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc drawChatCase) run(t *testing.T) {
	pub := &variablePublisher{}
	deps := engine.Deps{
		Proj: &fakeProj{modules: tc.views, commands: map[string]projection.Command{
			tc.command: {Name: tc.command, IsActive: true, Response: "custom " + tc.command, Perm: "everyone"},
		}},
		Draw: &tc.draw, Raffle: &tc.raffle, Queue: &fakeQueue{open: true}, Pub: pub,
		Live: &fakeLive{live: true}, Greet: &fakeGreet{}, Cooldown: engine.NoopCooldown{},
		Special: engine.NewSpecialSet(""), Log: zap.NewNop(),
	}
	p := engine.NewPipeline(deps, engine.NewRegistry(deps.Log, All(deps)...), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(p.Close)
	event := lane.Envelope{Type: "channel.chat.message", Lane: "standard", BroadcasterUserID: "2",
		BroadcasterUserLogin: "streamer", ChatterUserID: "9", ChatterUserLogin: "viewer", Text: "!" + tc.command}
	body, err := codec.Marshal(event)
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("draw-command", body)))
	tc.check(t, pub)
}

func (tc drawChatCase) check(t *testing.T, pub *variablePublisher) {
	t.Helper()
	got := drawChatResult{joined: tc.draw.joined, claimed: tc.draw.claimed,
		legacyJoined: tc.raffle.joinedIDs, legacyClaim: tc.raffle.claimedBy, message: tc.want.message}
	require.Equal(t, tc.want, got)
	if tc.want.message == "" {
		require.Empty(t, pub.messages)
		return
	}
	require.Len(t, pub.messages, 1)
	var payload struct{ Message string }
	require.NoError(t, codec.Unmarshal(pub.messages[0].Payload, &payload))
	require.Contains(t, payload.Message, tc.want.message)
}
