// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/rpc"
	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const giveawayRPCRoundID = "74b24fbb-c77d-4733-9a2c-bb64cbe38793"

type giveawayRPCProjection struct {
	projection.Reader
}

func (giveawayRPCProjection) User(context.Context, uint64) (projection.User, error) {
	return projection.User{Locale: "en"}, nil
}

func (giveawayRPCProjection) Module(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	config := codec.RawMessage(`{"openedEnabled":false,"presets":[{"name":"regulars","rules":{"min_watch_seconds":300}},{"name":"frozen","rules":{"min_points":10},"freezeWhileOpen":false}]}`)
	return projection.ModuleView{Name: engine.GiveawayModuleName, IsEnabled: true, Configs: config}, true, nil
}

type giveawayRPCClock struct{ engine.LoyaltyTicker }

func (giveawayRPCClock) AccrualHealth(context.Context, uint64) engine.AccrualState {
	return engine.AccrualHealthy
}

type giveawayRPCCalls struct {
	engine.DrawStore
	engine.GiveawayWinnersStore
	engine.StandingsSnapshots
	calls chan any
}

func (d giveawayRPCCalls) SnapshotCreate(context.Context, engine.SnapshotRequest) (engine.SnapshotRef, error) {
	return engine.SnapshotRef{SnapshotID: "snapshot"}, nil
}

func (d giveawayRPCCalls) Live(_ context.Context, channel draw.ChannelID) ([]draw.RoundView, error) {
	if channel != 42 {
		return nil, nil
	}
	round := rpcGiveawayRound(draw.OpenRequest{ID: giveawayRPCRoundID, Channel: channel, Terms: draw.Terms{
		Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "Game"}, Winners: draw.WinnerPolicy{Count: 2},
		Rules: codec.RawMessage(`{"min_follow_days":7}`),
	}})
	slot := draw.Slot{N: 1, Generation: 2, State: draw.SlotWon, Viewer: "17", Login: "winner",
		Deadline: round.ClosesAt, History: []draw.Candidate{{Viewer: "16", Login: "first", Generation: 1, Reason: "follow_age"}}}
	return []draw.RoundView{{Round: round, Slots: []draw.Slot{slot}, Entries: 9, Refused: 3},
		{Round: draw.Round{Terms: draw.Terms{Kind: draw.KindRaffle}}}}, nil
}

func (d giveawayRPCCalls) Joinable(context.Context, draw.ChannelID) (draw.Round, bool, error) {
	return draw.Round{}, false, nil
}

func (d giveawayRPCCalls) Open(_ context.Context, req draw.OpenRequest) (draw.Round, draw.OpenOutcome, error) {
	d.calls <- req
	return rpcGiveawayRound(req), draw.Opened, nil
}

func rpcGiveawayRound(req draw.OpenRequest) draw.Round {
	openedAt := req.OpenedAt
	if openedAt.IsZero() {
		openedAt = time.UnixMilli(1790000000000)
	}
	return draw.Round{ID: req.ID, Channel: req.Channel, Terms: req.Terms, Phase: draw.PhaseOpen,
		OpenedAt: openedAt, ClosesAt: openedAt.Add(req.Duration)}
}

func (d giveawayRPCCalls) Close(_ context.Context, req draw.CloseRequest) (bool, error) {
	return d.roundChange(req)
}

func (d giveawayRPCCalls) Extend(_ context.Context, req draw.ExtendRequest) (bool, error) {
	if req.Actor == "8" {
		return false, draw.ErrSnapshotExpiry
	}
	return d.roundChange(req)
}

func (d giveawayRPCCalls) Cancel(_ context.Context, req draw.ModAction) (bool, error) {
	return d.roundChange(req)
}

func (d giveawayRPCCalls) roundChange(req any) (bool, error) {
	d.calls <- req
	return true, nil
}

type giveawaySlotChange struct {
	verb   string
	action draw.SlotAction
}

func (d giveawayRPCCalls) Reroll(_ context.Context, req draw.SlotAction) (int, error) {
	return d.slotChange(drawrpc.VerbGiveawayReroll, req)
}

func (d giveawayRPCCalls) EndSlots(_ context.Context, req draw.SlotAction) (int, error) {
	return d.slotChange(drawrpc.VerbGiveawayEndSlots, req)
}

func (d giveawayRPCCalls) SkipRule(_ context.Context, req draw.SlotAction) (int, error) {
	return d.slotChange(drawrpc.VerbGiveawaySkipRule, req)
}

func (d giveawayRPCCalls) slotChange(verb string, req draw.SlotAction) (int, error) {
	d.calls <- giveawaySlotChange{verb: verb, action: req}
	return 1, nil
}

type giveawayDeliveryCall struct {
	channel  uint64
	delivery modulesrpc.GiveawayDelivery
}

func (d giveawayRPCCalls) MarkDelivered(_ context.Context, channel uint64, delivery modulesrpc.GiveawayDelivery) (bool, error) {
	return d.roundChange(giveawayDeliveryCall{channel, delivery})
}

type giveawayStandingsCall struct {
	channel uint64
	mins    engine.BalanceMinimums
}

func (d giveawayRPCCalls) BalanceCountAtLeast(_ context.Context, channel uint64, mins engine.BalanceMinimums) (loyaltyrpc.BalanceCount, error) {
	d.calls <- giveawayStandingsCall{channel, mins}
	return loyaltyrpc.BalanceCount{Count: 9}, nil
}

type giveawayRPCTest struct {
	verb     string
	edit     func(*drawrpc.GiveawayRequest)
	call     any
	response func(*testing.T, drawrpc.GiveawayReply, any)
	refusal  rpc.Code
	reason   string
}

func TestGiveawayRPC(t *testing.T) {
	nc := testnats.Connect(t)
	log := zap.NewNop()
	d := giveawayRPCCalls{calls: make(chan any, 1)}
	server := engine.GiveawayRPC{Deps: engine.Deps{
		Draw: d, LoyaltyTick: giveawayRPCClock{}, GiveawayWinners: d, StandingsSnapshots: d, Proj: giveawayRPCProjection{}, Automod: automod.New(), Log: log,
	}, Standings: d}
	require.NoError(t, server.Subscribe(bus.RPCWiring{NC: nc, Log: log}))
	for _, tc := range giveawayRPCCases() {
		t.Run(tc.verb, func(t *testing.T) {
			req := drawrpc.GiveawayRequest{UserID: "42", Source: "dashboard", ActorID: "7", RoundID: giveawayRPCRoundID, Slot: 2}
			if tc.edit != nil {
				tc.edit(&req)
			}
			reply, err := bus.RequestJSONTimeout[drawrpc.GiveawayReply](t.Context(), nc, drawrpc.GiveawayPrefix+"."+tc.verb, req, time.Second)
			tc.check(t, reply, err, rpcObservedCall(d.calls))
		})
	}
}

func rpcObservedCall(calls <-chan any) any {
	select {
	case call := <-calls:
		return call
	default:
		return nil
	}
}

func giveawayRPCCases() []giveawayRPCTest {
	slot := draw.SlotAction{Channel: 42, Actor: "7", Slot: 2, Round: giveawayRPCRoundID, Kind: draw.KindGiveaway}
	return []giveawayRPCTest{
		{verb: drawrpc.VerbGiveawayLive, response: checkGiveawayLive},
		{verb: drawrpc.VerbGiveawayLive, edit: func(r *drawrpc.GiveawayRequest) { r.UserID = "43" }, response: checkEmptyGiveawayLive},
		{verb: drawrpc.VerbGiveawayLive, edit: func(r *drawrpc.GiveawayRequest) { r.Source = "chat" }, refusal: rpc.CodeInvalid},
		{verb: drawrpc.VerbGiveawayLive, edit: func(r *drawrpc.GiveawayRequest) { r.UserID = "0" }, refusal: rpc.CodeInvalid},
		{verb: drawrpc.VerbGiveawayStart, edit: func(r *drawrpc.GiveawayRequest) { r.Prize, r.Winners, r.WindowSeconds = "Game", 2, 90 }, response: checkGiveawayStart},
		{verb: drawrpc.VerbGiveawayStart, edit: func(r *drawrpc.GiveawayRequest) { r.Prize, r.Preset = "Game", "frozen" },
			response: checkGiveawaySnapshotStart},
		{verb: drawrpc.VerbGiveawayStart, edit: func(r *drawrpc.GiveawayRequest) { r.Prize, r.WindowSeconds = "Game", -1 },
			refusal: rpc.CodeInvalid, reason: "giveaway.open.window"},
		{verb: drawrpc.VerbGiveawayStart, edit: func(r *drawrpc.GiveawayRequest) { r.Prize, r.WindowSeconds = "Game", 7201 },
			refusal: rpc.CodeInvalid, reason: "giveaway.open.window"},
		{verb: drawrpc.VerbGiveawayStart, edit: func(r *drawrpc.GiveawayRequest) { r.Prize, r.WindowSeconds = "Game", 1<<55 },
			refusal: rpc.CodeInvalid, reason: "giveaway.open.window"},
		{verb: drawrpc.VerbGiveawayClose, call: draw.CloseRequest{Channel: 42, Actor: "7", Round: giveawayRPCRoundID, Kind: draw.KindGiveaway}},
		{verb: drawrpc.VerbGiveawayClose, edit: func(r *drawrpc.GiveawayRequest) { r.ActorID = "0" }, refusal: rpc.CodeInvalid},
		{verb: drawrpc.VerbGiveawayClose, edit: func(r *drawrpc.GiveawayRequest) { r.RoundID = "regulars" },
			refusal: rpc.CodeInvalid, reason: "invalid round_id"},
		{verb: drawrpc.VerbGiveawayExtend, call: draw.ExtendRequest{Channel: 42, Actor: "7", Round: giveawayRPCRoundID, Kind: draw.KindGiveaway}},
		{verb: drawrpc.VerbGiveawayExtend, edit: func(r *drawrpc.GiveawayRequest) { r.ActorID = "8" },
			refusal: rpc.CodeConflict, reason: "adding a minute would exceed the standings snapshot expiry"},
		{verb: drawrpc.VerbGiveawayCancel, call: draw.ModAction{Channel: 42, Actor: "7", Round: giveawayRPCRoundID, Kind: draw.KindGiveaway}},
		{verb: drawrpc.VerbGiveawayReroll, call: giveawaySlotChange{drawrpc.VerbGiveawayReroll, slot}},
		{verb: drawrpc.VerbGiveawayEndSlots, call: giveawaySlotChange{drawrpc.VerbGiveawayEndSlots, slot}},
		{verb: drawrpc.VerbGiveawaySkipRule, call: giveawaySlotChange{drawrpc.VerbGiveawaySkipRule, slot}},
		{verb: drawrpc.VerbGiveawaySkipRule, edit: func(r *drawrpc.GiveawayRequest) { r.Slot = 0 }, refusal: rpc.CodeInvalid},
		{verb: drawrpc.VerbGiveawayMarkDelivered, edit: func(r *drawrpc.GiveawayRequest) { v := false; r.Delivered = &v },
			call: giveawayDeliveryCall{42, modulesrpc.GiveawayDelivery{RoundID: giveawayRPCRoundID, Slot: 2}}},
		{verb: drawrpc.VerbGiveawayMarkDelivered, refusal: rpc.CodeInvalid},
		{verb: drawrpc.VerbGiveawayEligibleCount, edit: func(r *drawrpc.GiveawayRequest) { r.Preset = "regulars" },
			call: giveawayStandingsCall{42, engine.BalanceMinimums{WatchSeconds: 300}}, response: checkGiveawayCount},
	}
}

func (tc giveawayRPCTest) check(t *testing.T, reply drawrpc.GiveawayReply, err error, observed any) {
	t.Helper()
	switch {
	case tc.refusal != rpc.CodeOK:
		var refusal bus.RPCReplyError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, tc.refusal, refusal.Code)
		if tc.reason != "" {
			require.Equal(t, tc.reason, refusal.Message)
		}
		require.Nil(t, observed)
	case tc.response != nil:
		require.NoError(t, err)
		tc.response(t, reply, observed)
	default:
		require.NoError(t, err)
		want := drawrpc.GiveawayReply{Done: true}
		if _, ok := tc.call.(giveawaySlotChange); ok {
			want.Affected = 1
		}
		require.Equal(t, want, reply)
		require.Equal(t, tc.call, observed)
	}
}

func checkGiveawayLive(t *testing.T, reply drawrpc.GiveawayReply, _ any) {
	t.Helper()
	require.NotNil(t, reply.Rounds)
	require.Equal(t, []drawrpc.GiveawayRound{{
		ID: giveawayRPCRoundID, Kind: "giveaway", Phase: "open", Prize: "Game", Winners: 2, OpenedMS: 1790000000000,
		ClosesMS: 1790000000000, Entries: 9, Refused: 3, Requirements: []string{"7 days of following"},
		Slots: []drawrpc.GiveawaySlot{{N: 1, Generation: 2, State: "won", ViewerID: "17", Login: "winner", DeadlineMS: 1790000000000,
			Rejected: []drawrpc.GiveawayCandidate{{ViewerID: "16", Login: "first", Generation: 1, Reason: "follow_age"}}}},
	}}, *reply.Rounds)
}

func checkEmptyGiveawayLive(t *testing.T, reply drawrpc.GiveawayReply, _ any) {
	t.Helper()
	require.NotNil(t, reply.Rounds)
	require.Equal(t, []drawrpc.GiveawayRound{}, *reply.Rounds)
}

func checkGiveawayStart(t *testing.T, reply drawrpc.GiveawayReply, observed any) {
	t.Helper()
	req, ok := observed.(draw.OpenRequest)
	require.True(t, ok)
	require.Equal(t, draw.OpenRequest{ID: req.ID, Channel: 42, Actor: "7", Duration: 90 * time.Second, Terms: draw.Terms{
		Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "Game"}, Winners: draw.WinnerPolicy{Count: 2},
		ClaimWindow: 2 * time.Minute, Rules: codec.RawMessage(`{}`),
	}}, req)
	require.Equal(t, []any{true, string(req.ID)}, []any{req.ID != "", reply.Round.ID})
}

func checkGiveawaySnapshotStart(t *testing.T, reply drawrpc.GiveawayReply, observed any) {
	t.Helper()
	req, ok := observed.(draw.OpenRequest)
	require.True(t, ok)
	require.Equal(t, []any{"snapshot", true, string(req.ID), req.OpenedAt.UnixMilli()},
		[]any{req.Terms.SnapshotID, !req.OpenedAt.IsZero(), reply.Round.ID, reply.Round.OpenedMS})
}

func checkGiveawayCount(t *testing.T, reply drawrpc.GiveawayReply, observed any) {
	t.Helper()
	count := int64(9)
	require.Equal(t, drawrpc.GiveawayReply{Count: &count}, reply)
	require.Equal(t, giveawayStandingsCall{42, engine.BalanceMinimums{WatchSeconds: 300}}, observed)
}
