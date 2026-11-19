// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
)

func TestBalanceCreditInvalidatesCachedStanding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reply    string
		replayed bool
	}{
		{name: "fresh credit", reply: `{"balance":{"viewer_id":"7","points":8}}`},
		{name: "replay", reply: `{"balance":{"viewer_id":"7","points":8},"replayed":true}`, replayed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc := testnats.Connect(t)
			bustest.Respond(t, nc, counterRPCPrefix+".balance.get", `{"balance":{"viewer_id":"7","points":5}}`, `{"balance":{"viewer_id":"7","points":12}}`)
			bustest.Respond(t, nc, counterRPCPrefix+".top.get", `{"top":[{"viewer_id":"7","points":5}]}`, `{"top":[{"viewer_id":"7","points":12}]}`)
			bustest.Respond(t, nc, counterRPCPrefix+".balance.credit", tc.reply)
			store := NewValkeyLoyaltyStore(newHotPathTestClient(t), NewLoyaltyRPC(nc, counterRPCPrefix), nil, nil)
			t.Cleanup(store.Close)
			credit := PointCredit{BroadcasterID: freshChannel(), ViewerID: 7, Value: 3, Key: "raffle:one:1", AccountCreatedAt: 11}
			for range 2 {
				require.Equal(t, [2]int64{5, 5}, creditStanding(t, store, credit))
			}

			outcome, err := store.BalanceCredit(t.Context(), credit)
			require.NoError(t, err)
			require.Equal(t, CreditOutcome{Balance: loyaltyrpc.Balance{ViewerID: "7", Points: 8}, Replayed: tc.replayed}, outcome)
			require.Equal(t, [2]int64{12, 12}, creditStanding(t, store, credit))
		})
	}
}

func creditStanding(t *testing.T, store *ValkeyLoyaltyStore, credit PointCredit) [2]int64 {
	t.Helper()
	balance, err := store.BalanceGet(t.Context(), credit.BroadcasterID, credit.ViewerID)
	require.NoError(t, err)
	top, err := store.Top(t.Context(), credit.BroadcasterID, 10)
	require.NoError(t, err)
	require.Len(t, top, 1)
	return [2]int64{balance.Points, top[0].Points}
}
