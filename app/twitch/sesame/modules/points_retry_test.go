// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type retryPointsStore struct {
	fakeLoyalty
	err error
}

func TestPointsCommandsReplyOnFinalFailure(t *testing.T) {
	for _, command := range []string{"add", "give"} {
		t.Run(command, func(t *testing.T) {
			store := &retryPointsStore{err: errors.New("terminal store failure")}
			m := Loyalty(engine.Deps{Loyalty: store, Log: zap.NewNop()})
			c := loyaltyChat(streamer, "")
			args := command + " @bagelfan 50"
			c.Env.Text, c.Env.Mentions = "!points "+args, textMentions(args)
			msg := bus.NewMessage("final-points", nil)
			msg.MarkFinalDelivery()
			ctx := msg.WithDeliveryContext(t.Context())
			var replies collector
			err := findCmd(t, m, "points").Run(ctx, c, args, replies.emit)
			require.ErrorIs(t, err, store.err)
			require.Len(t, replies.out, 1)
		})
	}
}

func (s *retryPointsStore) BalanceAdjustViewer(context.Context, engine.BalanceAdjustment) (loyaltyrpc.Balance, bool, error) {
	return loyaltyrpc.Balance{Points: 500}, true, s.err
}

func (s *retryPointsStore) BalanceTransfer(context.Context, engine.PointTransfer) (loyaltyrpc.Balance, bool, bool, error) {
	return loyaltyrpc.Balance{Points: 500}, true, true, s.err
}

func TestPointsCommandsReplyOnlyAfterTerminalOutcome(t *testing.T) {
	for _, command := range []string{"add", "give"} {
		t.Run(command, func(t *testing.T) {
			store := &retryPointsStore{err: errors.New("temporary store failure")}
			m := Loyalty(engine.Deps{Loyalty: store, Log: zap.NewNop()})
			c := loyaltyChat(streamer, "")
			text := "!points " + command + " @bagelfan 50"
			out, err := runChatErr(t, m, c, text)
			require.ErrorIs(t, err, store.err)
			require.Empty(t, out)
			store.err = nil
			out, err = runChatErr(t, m, c, text)
			require.NoError(t, err)
			require.Len(t, out, 1)
		})
	}
}
