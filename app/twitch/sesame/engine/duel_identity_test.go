// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLedgerValue(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		stake   int64
		id      uint64
		wantErr bool
	}{
		{"100:42", 100, 42, false},
		{"100", 100, 0, false},
		{"100:x", 100, 0, true},
		{"junk:42", 0, 0, true},
	} {
		stake, id, err := parseLedgerValue(tc.raw)
		assert.Equal(t, tc.wantErr, err != nil, tc.raw)
		assert.Equal(t, tc.stake, stake, tc.raw)
		assert.Equal(t, tc.id, id, tc.raw)
	}
}

type recordingLoyalty struct {
	LoyaltyStore
	spends  []PointSpend
	viewers []BalanceAdjustment
}

func (r *recordingLoyalty) BalanceSpend(_ context.Context, spend PointSpend) (loyaltyrpc.Balance, bool, bool, error) {
	r.spends = append(r.spends, spend)
	return loyaltyrpc.Balance{}, true, true, nil
}

func (r *recordingLoyalty) BalanceAdjustViewer(_ context.Context, a BalanceAdjustment) (loyaltyrpc.Balance, bool, error) {
	r.viewers = append(r.viewers, a)
	return loyaltyrpc.Balance{}, true, nil
}

func TestLoyaltyWalletActsOnViewerID(t *testing.T) {
	store := &recordingLoyalty{}
	wallet := NewLoyaltyWallet(store)
	renamed := DuelStake{Login: "newname", ViewerID: 42, Stake: 10}

	_, _, err := wallet.Debit(t.Context(), 2, renamed)
	require.NoError(t, err)
	require.NoError(t, wallet.Credit(t.Context(), 2, renamed))

	assert.Equal(t, []PointSpend{{BroadcasterID: 2, ViewerID: 42, Login: "newname", Amount: 10}}, store.spends)
	assert.Equal(t, []BalanceAdjustment{{BroadcasterID: 2, ViewerID: 42, ViewerLogin: "newname", Value: 10}}, store.viewers)
}

func TestValkeyDuelWritesLegacySeatsOldPodsCanRead(t *testing.T) {
	store, _, id := duelStoreFor(t, map[string]int64{"alice": 500, "bob": 100})
	ctx := t.Context()
	_, err := store.Open(ctx, id, DuelOpenSpec{Kind: DuelPot, Opener: p("alice"), Stake: 100})
	require.NoError(t, err)
	joined, err := store.Join(ctx, id, p("bob"), 20)
	require.NoError(t, err)
	require.True(t, joined.Joined)

	client := newHotPathTestClient(t)
	raw, err := client.Do(ctx, client.B().Hgetall().Key(duelKey(duelEntriesPrefix, id)).Build()).AsStrMap()
	require.NoError(t, err)

	assert.Equal(t, map[string]string{"alice": "100", "bob": "20"}, raw)
}

func TestValkeyDuelLegacyLedgerEntriesStillRefund(t *testing.T) {
	store, wallet, id := duelStoreFor(t, map[string]int64{"alice": 500, "bob": 100})
	ctx := t.Context()
	_, err := store.Open(ctx, id, DuelOpenSpec{Kind: DuelPot, Opener: p("alice"), Stake: 100})
	require.NoError(t, err)
	client := newHotPathTestClient(t)
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(duelKey(duelEntriesPrefix, id)).
		FieldValue().FieldValue("bob", "20:2").FieldValue("carol", "5").Build()).Error())

	cancelled, err := store.Cancel(ctx, id, p("alice"), false)
	require.NoError(t, err)

	assert.Equal(t, DuelCancelResult{Found: true, Cancelled: true, Refunded: 3, Total: 125}, cancelled)
	assert.EqualValues(t, 500, wallet.snapshot()["alice"])
	assert.EqualValues(t, 120, wallet.snapshot()["bob"])
}

func TestParseDuelLedgerReadsEveryStoredFormat(t *testing.T) {
	ledger := map[string]string{
		"id:7":  "30:7:renamed",
		"bob":   "20:2",
		"carol": "5",
		"junk":  "points",
		"ghost": "0",
	}

	got := parseDuelLedger(ledger)

	assert.Equal(t, []DuelStake{
		{Login: "bob", ViewerID: 2, Stake: 20},
		{Login: "carol", Stake: 5},
		{Login: "renamed", ViewerID: 7, Stake: 30},
	}, got)
	assert.Equal(t, DuelStake{Login: "renamed", ViewerID: 7, Stake: 30}, PickDuelStake(got, 30))
}

func TestDuelSeatsUseTheLegacyLedgerFormat(t *testing.T) {
	seat := DuelStake{Login: "a", ViewerID: 7, Stake: 30}
	assert.Equal(t, "a", seat.ledgerField())
	assert.Equal(t, "30", seat.ledgerValue())
}

func TestValkeyDuelAcceptsByViewerIDAfterARename(t *testing.T) {
	store, _, id := duelStoreFor(t, map[string]int64{"alice": 100, "bob": 100})
	ctx := t.Context()
	_, err := store.Open(ctx, id, DuelOpenSpec{Kind: DuelChallenge, Opener: p("alice"), Challenged: p("bob"), Stake: 50})
	require.NoError(t, err)

	impostor, err := store.Accept(ctx, id, DuelParty{ID: duelViewerIDs["carol"], Login: "bob"})
	require.NoError(t, err)

	assert.Equal(t, DuelAcceptResult{Found: true, WrongUser: true}, impostor, "taking a stale login does not grant the challenge")
}
