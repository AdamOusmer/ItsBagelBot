// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"math"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type bigintGambleLoyalty struct {
	fakeLoyalty
	points int64
}

func (f *bigintGambleLoyalty) BalanceGet(ctx context.Context, broadcasterID, viewerID uint64) (loyaltyrpc.Balance, error) {
	return loyaltyrpc.Balance{Points: f.points}, nil
}

func TestGambleRejectsBIGINTOverflowBeforeDebit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		balance int64
		amount  string
	}{
		{"doubled payout", math.MaxInt64/2 + 1, "4611686018427387904"},
		{"net winnings", math.MaxInt64, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &bigintGambleLoyalty{points: tc.balance}
			m := Gamble(engine.Deps{Loyalty: fake, Log: zap.NewNop()})
			out := runGames(t, m, gamesCtx("alice", `{"maxBet":9223372036854775807}`), tc.amount)
			require.Len(t, out, 1)
			require.Contains(t, out[0].Text, "could exceed")
			require.Empty(t, fake.wagers, "unsafe bets must not debit the wallet")
			require.Empty(t, fake.adjusts)
		})
	}
}

func TestGambleRollFailureDoesNotMovePoints(t *testing.T) {
	original := engine.RollGamble
	engine.RollGamble = func() (int64, error) { return 0, errors.New("dice unavailable") }
	t.Cleanup(func() { engine.RollGamble = original })
	fake := &fakeLoyalty{}
	m := Gamble(engine.Deps{Loyalty: fake, Log: zap.NewNop()})
	var col collector
	require.Error(t, m.Commands[0].Run(t.Context(), gamesCtx("alice", ""), "100", col.emit))
	require.Empty(t, fake.wagers)
}

type staleGambleBalance struct{ fakeLoyalty }

func (f *staleGambleBalance) BalanceGet(_ context.Context, _, _ uint64) (loyaltyrpc.Balance, error) {
	return loyaltyrpc.Balance{Points: math.MaxInt64 - 2}, nil
}
func (f *staleGambleBalance) BalanceWager(_ context.Context, wager engine.PointWager) (engine.WagerOutcome, error) {
	f.wagers = append(f.wagers, wager)
	return engine.WagerOutcome{Balance: loyaltyrpc.Balance{Points: math.MaxInt64}, Found: true, LimitExceeded: true}, nil
}
func TestGambleStaleCapacityRefusalDoesNotAnnounceWin(t *testing.T) {
	pinRoll(t, 1)
	fake := &staleGambleBalance{}
	m := Gamble(engine.Deps{Loyalty: fake, Log: zap.NewNop()})
	out := runGames(t, m, gamesCtx("alice", ""), "1")
	require.Len(t, out, 1)
	require.Contains(t, out[0].Text, "could exceed")
	require.NotContains(t, out[0].Text, "won")
	require.Len(t, fake.wagers, 1)
}
