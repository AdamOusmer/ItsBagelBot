// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/rpc"
	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type raffleMutationDraw struct {
	engine.DrawStore
	result draw.OpenOutcome
	prior  draw.Kind
	calls  chan any
}

func (d raffleMutationDraw) Open(_ context.Context, req draw.OpenRequest) (draw.Round, draw.OpenOutcome, error) {
	d.calls <- req
	req.ID = giveawayRPCRoundID
	return rpcGiveawayRound(req), d.result, nil
}

func (d raffleMutationDraw) Joinable(context.Context, draw.ChannelID) (draw.Round, bool, error) {
	return draw.Round{Terms: draw.Terms{Kind: d.prior}}, true, nil
}

func (d raffleMutationDraw) Close(_ context.Context, req draw.CloseRequest) (bool, error) {
	d.calls <- req
	return true, nil
}

func (d raffleMutationDraw) CancelRound(_ context.Context, req draw.ModAction) (draw.Round, bool, error) {
	d.calls <- req
	return draw.Round{}, true, nil
}

type raffleMutationProjection struct {
	projection.Reader
	off bool
}

func (p raffleMutationProjection) Module(_ context.Context, _ uint64, name string) (projection.ModuleView, bool, error) {
	return projection.ModuleView{Name: name, IsEnabled: !p.off,
		Configs: codec.RawMessage(`{"openedEnabled":"off","prizePoints":9500,"durationSeconds":45,"midCountEnabled":false}`)}, true, nil
}

type raffleMutationCase struct {
	name, verb, reason string
	req                drawrpc.RaffleRequest
	result             draw.OpenOutcome
	prior              draw.Kind
	off                bool
	wantCode           rpc.Code
	want               any
}

func TestRaffleDashboardMutations(t *testing.T) {
	for _, tc := range []raffleMutationCase{
		{name: "single defaults", verb: drawrpc.VerbRaffleStart, want: raffleRPCOpen(1, false, false)},
		{name: "split defaults", verb: drawrpc.VerbRaffleStart, req: drawrpc.RaffleRequest{Split: true},
			want: raffleRPCOpen(3, false, true)},
		{name: "split auto", verb: drawrpc.VerbRaffleStart, req: drawrpc.RaffleRequest{Split: true, Auto: true},
			want: raffleRPCOpen(3, true, true)},
		{name: "invalid duration", verb: drawrpc.VerbRaffleStart, req: drawrpc.RaffleRequest{Seconds: 4}, reason: "invalid", wantCode: rpc.CodeInvalid},
		{name: "invalid actor", verb: drawrpc.VerbRaffleStart, req: drawrpc.RaffleRequest{ActorID: "0"}, reason: "invalid", wantCode: rpc.CodeInvalid},
		{name: "invalid channel", verb: drawrpc.VerbRaffleStart, req: drawrpc.RaffleRequest{UserID: "0"}, reason: "invalid", wantCode: rpc.CodeInvalid},
		{name: "loyalty off", verb: drawrpc.VerbRaffleStart, off: true, reason: "loyalty_off", wantCode: rpc.CodeForbidden},
		{name: "giveaway busy", verb: drawrpc.VerbRaffleStart, result: draw.JoinableBusy, prior: draw.KindGiveaway,
			want: raffleRPCOpen(1, false, false), reason: "busy_giveaway", wantCode: rpc.CodeConflict},
		{name: "raffle busy", verb: drawrpc.VerbRaffleStart, result: draw.LegacyBusy,
			want: raffleRPCOpen(1, false, false), reason: "busy_raffle", wantCode: rpc.CodeConflict},
		{name: "draw auto", verb: drawrpc.VerbRaffleDraw, req: drawrpc.RaffleRequest{RoundID: giveawayRPCRoundID, Auto: true},
			want: draw.CloseRequest{Channel: 42, Actor: "7", Round: giveawayRPCRoundID, Kind: draw.KindRaffle, Winners: &draw.WinnerPolicy{Auto: true}}},
		{name: "cancel", verb: drawrpc.VerbRaffleCancel, req: drawrpc.RaffleRequest{RoundID: giveawayRPCRoundID},
			want: draw.ModAction{Channel: 42, Actor: "7", Round: giveawayRPCRoundID, Kind: draw.KindRaffle}},
		{name: "cancel without round", verb: drawrpc.VerbRaffleCancel, reason: "invalid", wantCode: rpc.CodeInvalid},
	} {
		t.Run(tc.name, tc.run)
	}
}

func raffleRPCOpen(winners int, auto, split bool) draw.OpenRequest {
	return draw.OpenRequest{Channel: 42, Actor: "7", Duration: 45 * time.Second,
		Terms: draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 9500}, Winners: draw.WinnerPolicy{Count: winners, Auto: auto},
			MidCountDisabled: true, MultiRaffle: split}}
}

func (tc raffleMutationCase) run(t *testing.T) {
	nc := testnats.Connect(t)
	log := zap.NewNop()
	d := raffleMutationDraw{result: tc.result, prior: tc.prior, calls: make(chan any, 1)}
	s := engine.RaffleRPC{Deps: engine.Deps{Draw: d, Proj: raffleMutationProjection{off: tc.off}, Log: log}}
	require.NoError(t, s.Subscribe(bus.RPCWiring{NC: nc, Log: log}))
	req := tc.request()
	body, err := codec.Marshal(req)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	response, err := nc.RequestWithContext(ctx, drawrpc.RafflePrefix+"."+tc.verb, body)
	require.NoError(t, err)
	var reply drawrpc.RaffleReply
	require.NoError(t, codec.Unmarshal(response.Data, &reply))
	require.Equal(t, tc.want, rpcObservedCall(d.calls))
	tc.checkReply(t, reply)
}

func (tc raffleMutationCase) request() drawrpc.RaffleRequest {
	req := tc.req
	if req.UserID == "" {
		req.UserID = "42"
	}
	if req.ActorID == "" {
		req.ActorID = "7"
	}
	return req
}

func (tc raffleMutationCase) checkReply(t *testing.T, reply drawrpc.RaffleReply) {
	t.Helper()
	switch {
	case tc.reason != "":
		require.Equal(t, drawrpc.RaffleReply{Reason: tc.reason, Refusal: rpc.Refused(tc.wantCode, tc.reason)}, reply)
	case tc.verb == drawrpc.VerbRaffleStart:
		open := tc.want.(draw.OpenRequest)
		auto := open.Terms.Winners.Auto
		require.Equal(t, drawrpc.RaffleReply{RoundID: giveawayRPCRoundID, Amount: 9500, Auto: &auto,
			Winners: open.Terms.Winners.Count, ClosesMS: 1790000045000}, reply)
	default:
		require.Equal(t, drawrpc.RaffleReply{Done: true}, reply)
	}
}
