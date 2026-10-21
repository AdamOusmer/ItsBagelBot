// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func raffleStoreWith(t *testing.T, cfg RaffleConfig) (*ValkeyRaffleStore, uint64) {
	t.Helper()
	client := newHotPathTestClient(t)
	id := freshChannel()
	t.Cleanup(func() {
		ctx := context.Background()
		client.Do(ctx, client.B().Del().Key(
			raffleKey(raffleDeadlinePrefix, id), raffleKey(raffleStatePrefix, id), raffleKey(raffleEntriesPrefix, id),
			raffleKey(raffleRemindPrefix, id), raffleKey(raffleDrawPrefix, id), raffleKey(raffleLastPrefix, id),
			raffleKey(raffleNamesPrefix, id), raffleKey(raffleClaimPrefix, id),
		).Build())
		client.Do(ctx, client.B().Zrem().Key(raffleDueKey).Member(strconv.FormatUint(id, 10)).Build())
	})
	return NewValkeyRaffleStore(client, cfg, nil), id
}

func raffleStoreFor(t *testing.T) (*ValkeyRaffleStore, uint64) {
	return raffleStoreWith(t, RaffleConfig{})
}

func numberedEntrants(n int) []RaffleEntrant {
	entrants := make([]RaffleEntrant, n)
	for i := range entrants {
		entrants[i] = RaffleEntrant{ID: strconv.Itoa(1000 + i), Login: fmt.Sprintf("viewer%03d", i)}
	}
	return entrants
}

func entrantIDs(entrants []RaffleEntrant) []string {
	ids := make([]string, len(entrants))
	for i, e := range entrants {
		ids[i] = e.ID
	}
	return ids
}

func winnerIDs(winners []RaffleWinner) []string {
	ids := make([]string, len(winners))
	for i, w := range winners {
		ids[i] = w.ID
	}
	return ids
}

type raffleSetup struct {
	spec     RaffleOpenSpec
	entrants []RaffleEntrant
}

func openAndJoin(t *testing.T, store *ValkeyRaffleStore, id uint64, setup raffleSetup) {
	t.Helper()
	ctx := context.Background()
	opened, err := store.Open(ctx, id, setup.spec)
	require.NoError(t, err)
	require.True(t, opened)
	for _, e := range setup.entrants {
		entry, err := store.Join(ctx, id, e)
		require.NoError(t, err)
		require.True(t, entry.Joined)
	}
}

func TestValkeyRaffleDrawsDistinctWinnersFromTheEntrants(t *testing.T) {
	cases := []struct {
		name        string
		opened      int64
		entrants    int
		ask         int64
		wantWinners int
	}{
		{"the opening spec decides the winner count when the draw does not override it", 3, 10, 0, 3},
		{"a draw override beats the opening spec", 3, 10, 5, 5},
		{"an ask beyond the pool draws every entrant once", 3, 4, 50, 4},
		{"an unset opening spec draws a single winner", 0, 6, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, id := raffleStoreFor(t)
			entrants := numberedEntrants(tc.entrants)
			openAndJoin(t, store, id, raffleSetup{RaffleOpenSpec{OpenedBy: "streamer", Winners: tc.opened, Duration: time.Minute}, entrants})

			res, err := store.Draw(context.Background(), id, tc.ask)

			require.NoError(t, err)
			require.NotNil(t, res)
			ids := winnerIDs(res.Winners)
			assert.Len(t, ids, tc.wantWinners)
			assert.Subset(t, entrantIDs(entrants), ids, "winners come from the eligible entries")
			assert.Len(t, uniqueStrings(ids), tc.wantWinners, "no entrant wins twice")
			assert.EqualValues(t, tc.entrants, res.Entrants)
			assert.Equal(t, DigestPool(entrantIDs(entrants)), res.Digest, "the digest binds the exact pool in join order")
		})
	}
}

func uniqueStrings(items []string) map[string]bool {
	set := map[string]bool{}
	for _, item := range items {
		set[item] = true
	}
	return set
}

func TestValkeyRaffleClosedRaffleIsInert(t *testing.T) {
	store, id := raffleStoreFor(t)
	ctx := context.Background()

	entry, err := store.Join(ctx, id, RaffleEntrant{ID: "7", Login: "early"})
	require.NoError(t, err)
	drawn, err := store.Draw(ctx, id, 0)
	require.NoError(t, err)
	overridden, err := store.Draw(ctx, id, 3)
	require.NoError(t, err)

	assert.False(t, entry.Open, "joining a closed raffle does nothing")
	assert.Nil(t, drawn, "a closed raffle has nothing to draw")
	assert.Nil(t, overridden, "a winner count does not conjure a raffle")
}

func TestValkeyRaffleRefusesAJoinOnceTheDeadlinePassed(t *testing.T) {
	store, id := raffleStoreFor(t)
	ctx := context.Background()
	openAndJoin(t, store, id, raffleSetup{spec: RaffleOpenSpec{Duration: time.Minute}})
	require.NoError(t, store.client.Do(ctx, store.client.B().Del().Key(raffleKey(raffleDeadlinePrefix, id)).Build()).Error())

	entry, err := store.Join(ctx, id, RaffleEntrant{ID: "7", Login: "late"})

	require.NoError(t, err)
	assert.False(t, entry.Open)
	assert.False(t, entry.Joined)
}

func TestValkeyRaffleLifecycle(t *testing.T) {
	store, id := raffleStoreFor(t)
	ctx := context.Background()

	opened, err := store.Open(ctx, id, RaffleOpenSpec{OpenedBy: "streamer", Winners: 1, Duration: time.Minute})
	require.NoError(t, err)
	require.True(t, opened)
	reopened, err := store.Open(ctx, id, RaffleOpenSpec{OpenedBy: "other", Duration: time.Minute})
	require.NoError(t, err)
	assert.False(t, reopened, "a second open while one is running is refused")

	sam := RaffleEntrant{ID: "42", Login: "sam"}
	first, err := store.Join(ctx, id, sam)
	require.NoError(t, err)
	again, err := store.Join(ctx, id, RaffleEntrant{ID: "42", Login: "sam_renamed"})
	require.NoError(t, err)
	assert.Equal(t, RaffleEntry{Joined: true, Open: true, Entrants: 1}, first)
	assert.Equal(t, RaffleEntry{Joined: false, Open: true, Entrants: 1}, again, "a repeat join is not counted twice")
	status, err := store.Status(ctx, id)
	require.NoError(t, err)
	assert.True(t, status.Open)
	assert.EqualValues(t, 1, status.Entrants)

	res, err := store.Draw(ctx, id, 0)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, []RaffleWinner{{ID: "42", Login: "sam_renamed"}}, res.Winners, "the latest login names the winner")

	last, found, err := store.LastResult(ctx, id)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, res.Winners, last.Winners)
	closed, err := store.Status(ctx, id)
	require.NoError(t, err)
	assert.False(t, closed.Open, "a drawn raffle is closed")

	claim, err := store.Claim(ctx, id, "42")
	require.NoError(t, err)
	assert.Equal(t, ClaimOk, claim, "claims match the winner by viewer id")
	t.Cleanup(func() {
		client := newHotPathTestClient(t)
		client.Do(ctx, client.B().Del().Key(raffleSnapPrefix+strconv.FormatUint(id, 10)+":"+strconv.FormatInt(res.DrawnAt, 10)).Build())
	})
}

type fakeRaffleWallet struct {
	mu      sync.Mutex
	credits []RaffleWinner
	fail    map[string]bool
}

func (w *fakeRaffleWallet) Credit(_ context.Context, _ uint64, winner RaffleWinner) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail[winner.ID] {
		return errors.New("loyalty down")
	}
	w.credits = append(w.credits, winner)
	return nil
}

func loyaltyReader(loyaltyOn bool, raffleConfigs string) fakeReader {
	modules := map[string]projection.ModuleView{
		RaffleModuleName: {IsEnabled: true, Configs: []byte(raffleConfigs)},
	}
	if loyaltyOn {
		modules[LoyaltyModuleName] = projection.ModuleView{IsEnabled: true, Configs: []byte(`{"pointsName":"bagels"}`)}
	}
	return fakeReader{modules: modules}
}

func TestValkeyRaffleSplitsThePrizeAcrossTheWinners(t *testing.T) {
	wallet := &fakeRaffleWallet{}
	cfg := RaffleConfig{AnnouncementConfig: AnnouncementConfig{Proj: loyaltyReader(true, "")}, Wallet: wallet}
	store, id := raffleStoreWith(t, cfg)
	openAndJoin(t, store, id, raffleSetup{RaffleOpenSpec{Winners: 3, Duration: time.Minute, Prize: 1000}, numberedEntrants(5)})

	res, err := store.Draw(context.Background(), id, 0)

	require.NoError(t, err)
	assert.Equal(t, RafflePayoutPaid, res.Payout)
	var paid int64
	for _, w := range wallet.credits {
		paid += w.Points
	}
	assert.EqualValues(t, 1000, paid, "the shares add up to the whole pot")
	assert.Equal(t, []int64{334, 333, 333}, []int64{res.Winners[0].Points, res.Winners[1].Points, res.Winners[2].Points})
	last, _, err := store.LastResult(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, RafflePayoutPaid, last.Payout, "the receipt records the payout")
}

func TestValkeyRaffleReportsAPrizeItCouldNotPay(t *testing.T) {
	cases := []struct {
		name      string
		loyaltyOn bool
		fail      map[string]bool
		want      RafflePayout
		credits   int
	}{
		{name: "loyalty off pays nothing", want: RafflePayoutLoyaltyOff},
		{name: "a failing credit is reported", loyaltyOn: true, fail: map[string]bool{"1000": true}, want: RafflePayoutFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wallet := &fakeRaffleWallet{fail: tc.fail}
			cfg := RaffleConfig{AnnouncementConfig: AnnouncementConfig{Proj: loyaltyReader(tc.loyaltyOn, "")}, Wallet: wallet}
			store, id := raffleStoreWith(t, cfg)
			openAndJoin(t, store, id, raffleSetup{RaffleOpenSpec{Duration: time.Minute, Prize: 1000}, numberedEntrants(1)})

			res, err := store.Draw(context.Background(), id, 0)

			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Payout)
			assert.Len(t, wallet.credits, tc.credits)
		})
	}
}

func TestRaffleWalletCreditsTheWinnerByViewerID(t *testing.T) {
	loyalty := &recordingLoyalty{}

	err := NewRaffleWallet(loyalty).Credit(t.Context(), 2, RaffleWinner{ID: "42", Login: "first_timer", Points: 1000})

	require.NoError(t, err)
	assert.Equal(t, []BalanceAdjustment{{BroadcasterID: 2, ViewerID: 42, ViewerLogin: "first_timer", Value: 1000}}, loyalty.viewers)
	assert.Error(t, NewRaffleWallet(loyalty).Credit(t.Context(), 2, RaffleWinner{ID: "legacy_login", Points: 5}))
}

func TestValkeyRaffleSweepDrawsARaffleWhoseExpiryWasMissed(t *testing.T) {
	pub := &fakePublisher{}
	wallet := &fakeRaffleWallet{}
	proj := loyaltyReader(true, `{"wonMessage":"{targets} grabbed {amount} {points}!"}`)
	cfg := RaffleConfig{AnnouncementConfig: AnnouncementConfig{Proj: proj, Pub: pub, OutgressStandardSubject: standardSubj}, Wallet: wallet}
	store, id := raffleStoreWith(t, cfg)
	openAndJoin(t, store, id, raffleSetup{RaffleOpenSpec{Duration: time.Minute, Prize: 1000}, []RaffleEntrant{{ID: "42", Login: "sam"}}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	member := strconv.FormatUint(id, 10)
	require.NoError(t, store.client.Do(ctx, store.client.B().Zadd().Key(raffleDueKey).ScoreMember().ScoreMember(1, member).Build()).Error())
	require.NoError(t, store.client.Do(ctx, store.client.B().Del().Key(raffleKey(raffleDeadlinePrefix, id)).Build()).Error())

	go store.StartExpiryWatcher(ctx)

	require.Eventually(t, func() bool { return len(pub.chatTexts(t)) > 0 }, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, []string{"@sam grabbed 1000 bagels!"}, pub.chatTexts(t), "the timer path uses the broadcaster's won message")
	assert.Equal(t, []RaffleWinner{{ID: "42", Login: "sam", Points: 1000}}, wallet.credits)
	due, err := store.client.Do(ctx, store.client.B().Zscore().Key(raffleDueKey).Member(member).Build()).ToString()
	assert.Error(t, err, "a drawn raffle leaves the due index, got %q", due)
}
