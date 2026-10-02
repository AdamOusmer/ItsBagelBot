// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeWallet struct {
	mu       sync.Mutex
	balances map[string]int64
}

func (w *fakeWallet) Debit(_ context.Context, _ uint64, entry DuelStake) (found, spent bool, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	balance, known := w.balances[entry.Login]
	if !known || balance < entry.Stake {
		return known, false, nil
	}
	w.balances[entry.Login] = balance - entry.Stake
	return true, true, nil
}

func (w *fakeWallet) Credit(_ context.Context, _ uint64, entry DuelStake) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.balances[entry.Login] += entry.Stake
	return nil
}

func (w *fakeWallet) snapshot() map[string]int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]int64, len(w.balances))
	for login, balance := range w.balances {
		out[login] = balance
	}
	return out
}

func duelStoreFor(t *testing.T, balances map[string]int64) (*ValkeyDuelStore, *fakeWallet, uint64) {
	t.Helper()
	client := newHotPathTestClient(t)
	wallet := &fakeWallet{balances: balances}
	id := freshChannel()
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(
			duelKey(duelDeadlinePrefix, id), duelKey(duelStatePrefix, id), duelKey(duelEntriesPrefix, id),
			duelKey(duelDrawPrefix, id), duelKey(duelLastPrefix, id),
		).Build())
	})
	return NewValkeyDuelStore(client, DuelConfig{Wallet: wallet}, nil), wallet, id
}

func TestValkeyDuelOpenValidatesTheSpecAndTheOpenersBalance(t *testing.T) {
	cases := []struct {
		name        string
		spec        DuelOpenSpec
		wantErr     bool
		want        DuelOpenResult
		wantBalance int64
	}{
		{"a stake below the floor is refused", DuelOpenSpec{Kind: DuelPot, Opener: "alice", Stake: 0}, true, DuelOpenResult{}, 500},
		{"a stake above the ceiling is refused", DuelOpenSpec{Kind: DuelPot, Opener: "alice", Stake: DuelMaxStake + 1}, true, DuelOpenResult{}, 500},
		{"an opener is required", DuelOpenSpec{Kind: DuelPot, Stake: 10}, true, DuelOpenResult{}, 500},
		{"an unknown kind is refused", DuelOpenSpec{Kind: "melee", Opener: "alice", Stake: 10}, true, DuelOpenResult{}, 500},
		{"a challenge needs a challenged party", DuelOpenSpec{Kind: DuelChallenge, Opener: "alice", Stake: 10}, true, DuelOpenResult{}, 500},
		{"a self-challenge is refused", DuelOpenSpec{Kind: DuelChallenge, Opener: "alice", Challenged: "alice", Stake: 10}, true, DuelOpenResult{}, 500},
		{"an opener who cannot cover the stake is short", DuelOpenSpec{Kind: DuelPot, Opener: "alice", Stake: 501}, false, DuelOpenResult{Short: true}, 500},
		{"an opener loyalty has never seen is unknown", DuelOpenSpec{Kind: DuelPot, Opener: "ghost", Stake: 10}, false, DuelOpenResult{Unknown: true}, 500},
		{"a valid pot escrows the opener's stake", DuelOpenSpec{Kind: DuelPot, Opener: "alice", Stake: 100}, false, DuelOpenResult{Started: true}, 400},
		{"a valid challenge escrows the opener's stake", DuelOpenSpec{Kind: DuelChallenge, Opener: "alice", Challenged: "bob", Stake: 100}, false, DuelOpenResult{Started: true}, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, wallet, id := duelStoreFor(t, map[string]int64{"alice": 500, "bob": 500})

			res, err := store.Open(context.Background(), id, tc.spec)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, res)
			assert.Equal(t, tc.wantBalance, wallet.snapshot()["alice"])
		})
	}
}

func TestValkeyDuelPotJoinsEscrowEachStakeOnce(t *testing.T) {
	store, wallet, id := duelStoreFor(t, map[string]int64{"alice": 500, "bob": 100, "carol": 5})
	ctx := context.Background()
	opened, err := store.Open(ctx, id, DuelOpenSpec{Kind: DuelPot, Opener: "alice", Stake: 100})
	require.NoError(t, err)
	require.True(t, opened.Started)
	joins := []struct {
		name   string
		login  string
		stake  int64
		want   DuelJoinResult
		wantOK bool
	}{
		{"a funded viewer joins", "bob", 20, DuelJoinResult{Open: true, Joined: true, Pot: 120, Entrants: 2}, true},
		{"a repeat join does not debit again", "bob", 20, DuelJoinResult{Open: true, Already: true, Pot: 120, Entrants: 2}, true},
		{"a viewer who cannot cover the stake is short", "carol", 30, DuelJoinResult{Open: true, Short: true, Pot: 120, Entrants: 2}, true},
		{"a viewer loyalty has never seen is unknown", "ghost", 30, DuelJoinResult{Open: true, Unknown: true, Pot: 120, Entrants: 2}, true},
		{"a zero stake is refused", "bob", 0, DuelJoinResult{}, false},
		{"a stake above the ceiling is refused", "bob", DuelMaxStake + 1, DuelJoinResult{}, false},
	}
	for _, step := range joins {
		got, err := store.Join(ctx, id, step.login, step.stake)

		assert.Equal(t, step.wantOK, err == nil, step.name)
		assert.Equal(t, step.want, got, step.name)
	}

	status, err := store.Status(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, DuelPot, status.Kind)
	assert.EqualValues(t, 120, status.Pot)
	assert.EqualValues(t, 2, status.Entrants)
	assert.Equal(t, map[string]int64{"alice": 400, "bob": 80, "carol": 5}, wallet.snapshot(), "each stake left its wallet exactly once")
}

func TestValkeyDuelCancelRefundsOnlyTheValidLedgerEntries(t *testing.T) {
	store, wallet, id := duelStoreFor(t, map[string]int64{"alice": 500, "bob": 100})
	ctx := context.Background()
	_, err := store.Open(ctx, id, DuelOpenSpec{Kind: DuelPot, Opener: "alice", Stake: 100})
	require.NoError(t, err)
	_, err = store.Join(ctx, id, "bob", 20)
	require.NoError(t, err)
	client := newHotPathTestClient(t)
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(duelKey(duelEntriesPrefix, id)).
		FieldValue().FieldValue("ghost", "0").FieldValue("junk", "points").Build()).Error())

	stranger, err := store.Cancel(ctx, id, "bob", false)
	require.NoError(t, err)
	cancelled, err := store.Cancel(ctx, id, "mod", true)
	require.NoError(t, err)

	assert.Equal(t, DuelCancelResult{Found: true}, stranger, "only the opener or a moderator may cancel")
	assert.Equal(t, DuelCancelResult{Found: true, Cancelled: true, Refunded: 2, Total: 120}, cancelled)
	assert.Equal(t, map[string]int64{"alice": 500, "bob": 100}, wallet.snapshot(), "every valid stake came back, junk entries paid nothing")
}

func TestValkeyDuelChallengeSettlement(t *testing.T) {
	cases := []struct {
		name       string
		openerWins bool
		want       map[string]int64
	}{
		{"the opener wins the coin flip and takes the pot", true, map[string]int64{"alice": 150, "bob": 50}},
		{"the challenged wins the coin flip and takes the pot", false, map[string]int64{"alice": 50, "bob": 150}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flip := FlipDuelCoin
			FlipDuelCoin = func() bool { return tc.openerWins }
			t.Cleanup(func() { FlipDuelCoin = flip })
			store, wallet, id := duelStoreFor(t, map[string]int64{"alice": 100, "bob": 100, "carol": 100})
			ctx := context.Background()
			_, err := store.Open(ctx, id, DuelOpenSpec{Kind: DuelChallenge, Opener: "alice", Challenged: "bob", Stake: 50})
			require.NoError(t, err)

			stranger, err := store.Accept(ctx, id, "carol")
			require.NoError(t, err)
			accepted, err := store.Accept(ctx, id, "bob")
			require.NoError(t, err)

			assert.Equal(t, DuelAcceptResult{Found: true, WrongUser: true}, stranger, "only the challenged party may accept")
			assert.True(t, accepted.Accepted)
			assert.EqualValues(t, 100, accepted.Pot)
			assert.EqualValues(t, 50, accepted.Stake)
			assert.Equal(t, tc.want, map[string]int64{"alice": wallet.snapshot()["alice"], "bob": wallet.snapshot()["bob"]})
			assert.EqualValues(t, 100, wallet.snapshot()["carol"], "a refused accept moves nothing")
		})
	}
}

func TestValkeyDuelChallengeRefusals(t *testing.T) {
	store, wallet, id := duelStoreFor(t, map[string]int64{"alice": 100, "bob": 10})
	ctx := context.Background()
	_, err := store.Open(ctx, id, DuelOpenSpec{Kind: DuelChallenge, Opener: "alice", Challenged: "bob", Stake: 50})
	require.NoError(t, err)

	short, err := store.Accept(ctx, id, "bob")
	require.NoError(t, err)
	wrong, err := store.Decline(ctx, id, "carol")
	require.NoError(t, err)
	declined, err := store.Decline(ctx, id, "bob")
	require.NoError(t, err)

	assert.Equal(t, DuelAcceptResult{Found: true, Short: true}, short, "a challenged party who cannot cover the stake leaves the duel open")
	assert.Equal(t, DuelDeclineResult{Found: true, WrongUser: true}, wrong)
	assert.Equal(t, DuelDeclineResult{Found: true, Declined: true, Opener: "alice", Refund: 50}, declined)
	assert.Equal(t, map[string]int64{"alice": 100, "bob": 10}, wallet.snapshot(), "a declined challenge refunds the opener in full")
}
