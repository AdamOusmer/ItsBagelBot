// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/db/modules/rpc"
	"ItsBagelBot/app/twitch/sesame/engine"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const giveawayRPCPrefix = "rpc.modules" + modulesrpc.GiveawaySuffix

func connectGiveawayRPC(t *testing.T) *nats.Conn {
	t.Helper()
	client := testdb.Open(t, "giveawayent", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	nc := testnats.Connect(t)
	wiring := bus.RPCWiring{NC: nc, Log: zap.NewNop()}
	require.NoError(t, rpc.SubscribeGiveaways(wiring, repository.NewGiveawayWinners(client), giveawayRPCPrefix))
	require.NoError(t, nc.Flush())
	return nc
}

func TestGiveawayRPCRoundTrip(t *testing.T) {
	nc := connectGiveawayRPC(t)
	client := engine.NewGiveawayWinnersRPC(nc, "rpc.modules")
	ctx := t.Context()
	wonAt := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	winner := modulesrpc.GiveawayWinner{
		RoundID: "round", Slot: 1, ViewerID: "42", Login: "viewer", Prize: "game",
		WonAt: wonAt.UnixMilli(), Claimed: true, ClaimedAt: wonAt.Add(time.Second).UnixMilli(),
	}
	require.NoError(t, client.Record(ctx, 10, []modulesrpc.GiveawayWinner{winner}))
	match, err := client.WonWithin(ctx, 10, []uint64{42, 43}, wonAt)
	require.NoError(t, err)
	require.Equal(t, []uint64{42}, match)

	mark := modulesrpc.GiveawayRequest{UserID: "10", RoundID: "round", Slot: 1, Delivered: true, DeliveryNote: "Sent"}
	reply, err := bus.RequestJSONTimeout[modulesrpc.GiveawayReply](ctx, nc, giveawayRPCPrefix+".deliver", mark, 3*time.Second)
	require.NoError(t, err)
	require.True(t, reply.Found)

	winners, err := client.Recent(ctx, 10, modulesrpc.GiveawayRecentQuery{RoundID: "round", Limit: 1})
	require.NoError(t, err)
	require.Len(t, winners, 1)
	require.Positive(t, winners[0].DeliveredAt)
	winner.DeliveredAt = winners[0].DeliveredAt
	winner.DeliveryNote = mark.DeliveryNote
	require.Equal(t, []modulesrpc.GiveawayWinner{winner}, winners)
}

func TestGiveawayRPCRejectsInvalidRequests(t *testing.T) {
	nc := connectGiveawayRPC(t)
	for _, tc := range []struct {
		name string
		verb string
		req  modulesrpc.GiveawayRequest
	}{
		{name: "zero channel", verb: "recent", req: modulesrpc.GiveawayRequest{UserID: "0"}},
		{name: "large page", verb: "recent", req: modulesrpc.GiveawayRequest{UserID: "10", Limit: modulesrpc.GiveawayMaxRecentPage + 1}},
		{name: "negative offset", verb: "recent", req: modulesrpc.GiveawayRequest{UserID: "10", Offset: -1}},
		{name: "missing slot", verb: "deliver", req: modulesrpc.GiveawayRequest{UserID: "10", RoundID: "round", Delivered: true}},
		{name: "invalid viewer", verb: "won_within", req: modulesrpc.GiveawayRequest{UserID: "10", SinceMs: 1, ViewerIDs: []string{"viewer"}}},
		{name: "missing cutoff", verb: "won_within", req: modulesrpc.GiveawayRequest{UserID: "10", ViewerIDs: []string{"42"}}},
		{name: "too many viewers", verb: "won_within", req: modulesrpc.GiveawayRequest{UserID: "10", SinceMs: 1, ViewerIDs: make([]string, modulesrpc.GiveawayMaxViewerIDs+1)}},
		{name: "too many winners", verb: "record", req: modulesrpc.GiveawayRequest{UserID: "10", Winners: make([]modulesrpc.GiveawayWinner, modulesrpc.GiveawayMaxWinners+1)}},
		{name: "invalid winner", verb: "record", req: modulesrpc.GiveawayRequest{UserID: "10", Winners: []modulesrpc.GiveawayWinner{{RoundID: "round", Slot: 1, ViewerID: "0", Prize: "game", WonAt: 1}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bus.RequestJSONTimeout[modulesrpc.GiveawayReply](t.Context(), nc, giveawayRPCPrefix+"."+tc.verb, tc.req, 3*time.Second)
			var replyError bus.RPCReplyError
			require.ErrorAs(t, err, &replyError)
			require.Equal(t, domainrpc.CodeInvalid, replyError.Code)
		})
	}
}
