// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/db/modules/rpc"
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
	wonAt := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	winner := modulesrpc.GiveawayWinner{
		RoundID: "round", Slot: 1, ViewerID: "42", Login: "viewer", Prize: "game",
		WonAt: wonAt.UnixMilli(), Claimed: true, ClaimedAt: wonAt.Add(time.Second).UnixMilli(),
	}
	record := modulesrpc.GiveawayRequest{UserID: "10", Winners: []modulesrpc.GiveawayWinner{winner}}
	_, err := requestGiveaway(t, nc, modulesrpc.VerbGiveawayRecord, record)
	require.NoError(t, err)

	lookup := modulesrpc.GiveawayRequest{UserID: "10", ViewerIDs: []string{"42", "43"}, SinceMs: wonAt.UnixMilli()}
	reply, err := requestGiveaway(t, nc, modulesrpc.VerbGiveawayWonWithin, lookup)
	require.NoError(t, err)
	require.Equal(t, []string{"42"}, reply.ViewerIDs)

	mark := modulesrpc.GiveawayRequest{UserID: "10", RoundID: "round", Slot: 1, Delivered: true}
	reply, err = requestGiveaway(t, nc, modulesrpc.VerbGiveawayDeliver, mark)
	require.NoError(t, err)
	require.True(t, reply.Found)

	recent := modulesrpc.GiveawayRequest{UserID: "10", RoundID: "round", Limit: 1}
	reply, err = requestGiveaway(t, nc, modulesrpc.VerbGiveawayRecent, recent)
	require.NoError(t, err)
	require.Len(t, reply.Winners, 1)
	winner.DeliveredAt = reply.Winners[0].DeliveredAt
	require.Positive(t, winner.DeliveredAt)
	require.Equal(t, []modulesrpc.GiveawayWinner{winner}, reply.Winners)
}

func requestGiveaway(t *testing.T, nc *nats.Conn, verb string, req modulesrpc.GiveawayRequest) (modulesrpc.GiveawayReply, error) {
	t.Helper()
	return bus.RequestJSONTimeout[modulesrpc.GiveawayReply](t.Context(), nc, giveawayRPCPrefix+"."+verb, req, 3*time.Second)
}

func TestGiveawayRPCRejectsInvalidRequests(t *testing.T) {
	nc := connectGiveawayRPC(t)
	winner := modulesrpc.GiveawayWinner{RoundID: "round", Slot: 1, ViewerID: "42", Login: "first", Prize: "game", WonAt: 1}
	batch := func(second modulesrpc.GiveawayWinner) modulesrpc.GiveawayRequest {
		return modulesrpc.GiveawayRequest{UserID: "10", Winners: []modulesrpc.GiveawayWinner{winner, second}}
	}
	mixedRounds := batch(modulesrpc.GiveawayWinner{RoundID: "other", Slot: 2, ViewerID: "43", Prize: "game", WonAt: 1})
	duplicateSlot := batch(modulesrpc.GiveawayWinner{RoundID: "round", Slot: 1, ViewerID: "43", Prize: "game", WonAt: 1})
	longLogin := batch(modulesrpc.GiveawayWinner{
		RoundID: "round", Slot: 2, ViewerID: "43", Prize: "game", WonAt: 1,
		Login: strings.Repeat("a", modulesrpc.GiveawayLoginMaxLen+1),
	})
	longPrize := batch(modulesrpc.GiveawayWinner{
		RoundID: "round", Slot: 2, ViewerID: "43", WonAt: 1,
		Prize: strings.Repeat("a", modulesrpc.GiveawayPrizeMaxLen+1),
	})

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
		{name: "mixed rounds", verb: "record", req: mixedRounds},
		{name: "duplicate slot", verb: "record", req: duplicateSlot},
		{name: "long login", verb: "record", req: longLogin},
		{name: "long prize", verb: "record", req: longPrize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := requestGiveaway(t, nc, tc.verb, tc.req)
			var replyError bus.RPCReplyError
			require.ErrorAs(t, err, &replyError)
			require.Equal(t, domainrpc.CodeInvalid, replyError.Code)
		})
	}
	reply, err := requestGiveaway(t, nc, modulesrpc.VerbGiveawayRecent, modulesrpc.GiveawayRequest{UserID: "10"})
	require.NoError(t, err)
	require.Empty(t, reply.Winners)
}
