// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGiveawayWinnersRPC(t *testing.T) {
	nc := testnats.Connect(t)
	wiring := bus.RPCWiring{NC: nc, Log: zap.NewNop()}
	client := engine.NewGiveawayWinnersRPC(nc, "test.modules")
	wonAt := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	winners := []modulesrpc.GiveawayWinner{{RoundID: "round", Slot: 1, ViewerID: "42", Login: "viewer", Prize: "game", WonAt: wonAt.UnixMilli()}}
	page := modulesrpc.GiveawayRecentQuery{RoundID: "round", Limit: 2, Offset: 1}

	for _, tc := range []struct {
		verb    string
		request modulesrpc.GiveawayRequest
		reply   modulesrpc.GiveawayReply
		call    func(context.Context) (any, error)
		want    any
	}{
		{
			verb:    "record",
			request: modulesrpc.GiveawayRequest{UserID: "10", Winners: winners},
			call:    func(ctx context.Context) (any, error) { return nil, client.Record(ctx, 10, winners) },
		},
		{
			verb:    "deliver",
			request: modulesrpc.GiveawayRequest{UserID: "10", RoundID: "round", Slot: 1, Delivered: true},
			reply:   modulesrpc.GiveawayReply{Found: true},
			call: func(ctx context.Context) (any, error) {
				return client.MarkDelivered(ctx, 10, modulesrpc.GiveawayDelivery{RoundID: "round", Slot: 1, Delivered: true})
			},
			want: true,
		},
		{
			verb:    "recent",
			request: modulesrpc.GiveawayRequest{UserID: "10", RoundID: "round", Limit: 2, Offset: 1},
			reply:   modulesrpc.GiveawayReply{Winners: winners},
			call:    func(ctx context.Context) (any, error) { return client.Recent(ctx, 10, page) },
			want:    winners,
		},
		{
			verb:    "won_within",
			request: modulesrpc.GiveawayRequest{UserID: "10", ViewerIDs: []string{"42", "43"}, SinceMs: wonAt.UnixMilli()},
			reply:   modulesrpc.GiveawayReply{ViewerIDs: []string{"42"}},
			call:    func(ctx context.Context) (any, error) { return client.WonWithin(ctx, 10, []uint64{42, 43}, wonAt) },
			want:    []uint64{42},
		},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			received := make(chan modulesrpc.GiveawayRequest, 1)
			err := bus.Serve(wiring, "test.modules.giveaway."+tc.verb, func(_ context.Context, req modulesrpc.GiveawayRequest) modulesrpc.GiveawayReply {
				received <- req
				return tc.reply
			})
			require.NoError(t, err)

			got, err := tc.call(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.request, <-received)
		})
	}
}
