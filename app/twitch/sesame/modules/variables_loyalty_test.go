// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type variableLoyaltyBalance struct {
	engine.LoyaltyStore
	balance loyaltyrpc.Balance
	reads   int
}

func (s *variableLoyaltyBalance) BalanceGet(context.Context, uint64, uint64) (loyaltyrpc.Balance, error) {
	s.reads++
	return s.balance, nil
}

type loyaltyVariableCase struct {
	name, locale, wantPoints string
	points                   int64
	seconds                  uint64
}

func TestLoyaltyVariablesMatchNativeRepliesAtBIGINTLimits(t *testing.T) {
	for _, tc := range []loyaltyVariableCase{
		{name: "above float precision", locale: "en", points: 9007199254740993, seconds: 7200, wantPoints: "9007199254740993"},
		{name: "signed BIGINT maximum", locale: "en", points: 1<<63 - 1, seconds: 9000, wantPoints: "9223372036854775807"},
		{name: "last whole-second duration", locale: "fr", points: 1<<63 - 1, seconds: uint64((1<<63 - 1) / time.Second), wantPoints: "9223372036854775807"},
		{name: "watch duration overflows", locale: "en", points: 1<<63 - 1, seconds: ^uint64(0), wantPoints: "9223372036854775807"},
		{name: "zero balance and duration", locale: "fr", wantPoints: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) { assertLoyaltyVariableReplies(t, tc) })
	}
}

func assertLoyaltyVariableReplies(t *testing.T, tc loyaltyVariableCase) {
	t.Helper()
	store := &variableLoyaltyBalance{balance: loyaltyrpc.Balance{Points: tc.points, PointsExact: tc.wantPoints, WatchSeconds: tc.seconds}}
	deps := engine.Deps{Loyalty: store, Log: zap.NewNop()}
	c := loyaltyCtx("channel.chat.message", "", `{"pointsName":"bagels"}`)
	c.Locale = tc.locale
	values, err := loyaltyVariables(deps)(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, tc.wantPoints, values["points"], "points must remain exact decimal BIGINT text")
	assert.Equal(t, values["duration"], values["watchtime"])
	assert.Equal(t, "bagels", values["pointsname"])
	points, watchtime := nativeLoyaltyVariableReplies(t, deps, c)
	palette := module.StringPalette(values)
	assert.Equal(t, points, palette.ExpandNamespaced("loyalty", i18n.T(c.Locale, "loyalty.points")))
	assert.Equal(t, watchtime, palette.ExpandNamespaced("loyalty", i18n.T(c.Locale, "loyalty.watchtime")))
	assert.Equal(t, 3, store.reads, "one balance read per custom/native response")
}

func nativeLoyaltyVariableReplies(t *testing.T, d engine.Deps, c *module.Context) (string, string) {
	t.Helper()
	var col collector
	native := loyaltyCmd{chatReplier: newChatReplier(c), d: d, emit: col.emit, log: d.Log}
	require.NoError(t, native.pointsShow(context.Background()))
	require.NoError(t, native.watchtimeShow(context.Background(), ""))
	require.Len(t, col.out, 2)
	return col.out[0].Text, col.out[1].Text
}
