// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
)

func TestKeyedBalanceInvalidatesCachedStanding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		verb  loyaltyVerb
		reply string
		want  DebitOutcome
	}{
		{name: "fresh credit", verb: loyaltyrpc.VerbBalanceCredit, reply: `{"balance":{"viewer_id":"7","points":8}}`,
			want: DebitOutcome{Balance: loyaltyrpc.Balance{ViewerID: "7", Points: 8}}},
		{name: "credit replay", verb: loyaltyrpc.VerbBalanceCredit, reply: `{"balance":{"viewer_id":"7","points":8},"replayed":true}`,
			want: DebitOutcome{Balance: loyaltyrpc.Balance{ViewerID: "7", Points: 8}, Replayed: true}},
		{name: "fresh debit", verb: loyaltyrpc.VerbBalanceDebit, reply: `{"balance":{"viewer_id":"7","points":2},"spent":true}`,
			want: DebitOutcome{Balance: loyaltyrpc.Balance{ViewerID: "7", Points: 2}, Spent: true}},
		{name: "debit replay", verb: loyaltyrpc.VerbBalanceDebit, reply: `{"balance":{"viewer_id":"7","points":2},"spent":true,"replayed":true}`,
			want: DebitOutcome{Balance: loyaltyrpc.Balance{ViewerID: "7", Points: 2}, Spent: true, Replayed: true}},
		{name: "debit missing replay", verb: loyaltyrpc.VerbBalanceDebit, reply: `{"replayed":true}`, want: DebitOutcome{Replayed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			bustest.Respond(t, nc, counterRPCPrefix+".balance.get", `{"balance":{"viewer_id":"7","points":5}}`, `{"balance":{"viewer_id":"7","points":12}}`)
			bustest.Respond(t, nc, counterRPCPrefix+".top.get", `{"top":[{"viewer_id":"7","points":5}]}`, `{"top":[{"viewer_id":"7","points":12}]}`)
			bustest.Respond(t, nc, counterRPCPrefix+"."+string(tc.verb), tc.reply)
			store := NewValkeyLoyaltyStore(newHotPathTestClient(t), NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
			t.Cleanup(store.Close)
			mutation := PointCredit{BroadcasterID: freshChannel(), ViewerID: 7, Value: 3, Key: "caller:one:1", AccountCreatedAt: 11}
			for range 2 {
				require.Equal(t, [2]int64{5, 5}, keyedStanding(t, store, mutation))
			}

			outcome, err := tryKeyedBalance(t.Context(), store, tc.verb, mutation)
			require.NoError(t, err)
			require.Equal(t, tc.want, outcome)
			require.Equal(t, [2]int64{12, 12}, keyedStanding(t, store, mutation))
		})
	}
}

func tryKeyedBalance(ctx context.Context, store *ValkeyLoyaltyStore, verb loyaltyVerb, mutation PointCredit) (DebitOutcome, error) {
	if verb == loyaltyrpc.VerbBalanceDebit {
		return store.BalanceDebit(ctx, PointDebit(mutation))
	}
	outcome, err := store.BalanceCredit(ctx, mutation)
	return DebitOutcome{Balance: outcome.Balance, Replayed: outcome.Replayed}, err
}

func keyedStanding(t *testing.T, store *ValkeyLoyaltyStore, mutation PointCredit) [2]int64 {
	t.Helper()
	balance, err := store.BalanceGet(t.Context(), mutation.BroadcasterID, mutation.ViewerID)
	require.NoError(t, err)
	top, err := store.Top(t.Context(), mutation.BroadcasterID, 10)
	require.NoError(t, err)
	require.Len(t, top, 1)
	return [2]int64{balance.Points, top[0].Points}
}
