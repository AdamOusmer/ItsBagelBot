// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	drawrpc "ItsBagelBot/internal/domain/rpc/draw"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type raffleStatusDraw struct {
	DrawStore
}

func (raffleStatusDraw) Live(_ context.Context, ch draw.ChannelID) ([]draw.RoundView, error) {
	if ch != 42 {
		return nil, nil
	}
	return []draw.RoundView{
		{Round: draw.Round{ID: "raffle", Phase: draw.PhaseSettling, ClosesAt: time.UnixMilli(123), Terms: RaffleTerms(RaffleLaunch{Points: 10})}, Entries: 4, Slots: []draw.Slot{
			{State: draw.SlotPaid, Login: "paid"}, {State: draw.SlotUnpaid, Login: "unpaid"},
		}},
		{Round: draw.Round{Terms: draw.Terms{Kind: draw.KindGiveaway}}, Slots: []draw.Slot{
			{State: draw.SlotEnded, Login: "giveaway"},
		}},
	}, nil
}

type raffleStatusWatch struct {
	LoyaltyTicker
}

func (raffleStatusWatch) AccrualHealth(context.Context, uint64) AccrualState { return AccrualHealthy }

func TestRaffleStatusRPC(t *testing.T) {
	nc := testnats.Connect(t)
	log := zap.NewNop()
	server := RaffleStatusRPC{Draw: raffleStatusDraw{}, Watch: raffleStatusWatch{}, Log: log}
	require.NoError(t, server.Subscribe(bus.RPCWiring{NC: nc, Log: log}))
	for _, tc := range []struct {
		name string
		req  drawrpc.RaffleStatusRequest
		want drawrpc.RaffleStatus
		code domainrpc.Code
	}{
		{"settling raffle", drawrpc.RaffleStatusRequest{UserID: "42", Source: "dashboard"},
			drawrpc.RaffleStatus{AccrualHealth: "healthy", Unpaid: []string{"unpaid"}, Rounds: []drawrpc.RaffleRound{{ID: "raffle", Phase: "settling", ClosesMS: 123, Entries: 4}}}, domainrpc.CodeOK},
		{"different channel", drawrpc.RaffleStatusRequest{UserID: "43", Source: "dashboard"},
			drawrpc.RaffleStatus{AccrualHealth: "healthy"}, domainrpc.CodeOK},
		{"invalid channel", drawrpc.RaffleStatusRequest{UserID: "0", Source: "dashboard"},
			drawrpc.RaffleStatus{}, domainrpc.CodeInvalid},
		{"invalid source", drawrpc.RaffleStatusRequest{UserID: "42", Source: "chat"},
			drawrpc.RaffleStatus{}, domainrpc.CodeInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bus.RequestJSONTimeout[drawrpc.RaffleStatus](t.Context(), nc, drawrpc.RaffleStatusSubject, tc.req, 0)
			if tc.code != domainrpc.CodeOK {
				var refusal bus.RPCReplyError
				require.ErrorAs(t, err, &refusal)
				require.Equal(t, tc.code, refusal.Code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
