// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics/analyticstest"
	"ItsBagelBot/internal/channelruntime"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRaffleAnalyticsCountsEachDrawOnce(t *testing.T) {
	for _, row := range []struct {
		entrants    int
		multi       bool
		end, bucket string
	}{
		{0, false, "empty", ""}, {1, false, "drawn", "1"}, {2, false, "drawn", "2_4"},
		{4, false, "drawn", "2_4"}, {5, false, "drawn", "5_19"}, {19, false, "drawn", "5_19"},
		{20, false, "drawn", "20_99"}, {99, false, "drawn", "20_99"}, {100, true, "drawn", "100p"},
	} {
		t.Run(fmt.Sprint(row.entrants), func(t *testing.T) {
			store, id := raffleStoreFor(t)
			capture := analyticstest.New(t)
			store.SetAnalytics(capture.Recorder)
			openAndJoin(t, store, id, raffleSetup{RaffleState{Multi: row.multi}, numberedEntrants(row.entrants)})
			_, err := store.Draw(t.Context(), id, 0)
			require.NoError(t, err)
			_, err = store.Draw(t.Context(), id, 0)
			require.NoError(t, err)
			game := "raffle"
			if row.multi {
				game = "mraffle"
			}
			want := map[string]int64{"games.rounds|" + game + "|" + row.end: 1}
			if row.bucket != "" {
				want["games.entrants|"+game+"|"+row.bucket] = 1
			}
			require.Equal(t, want, capture.Counters())
		})
	}
}

func TestDuelAnalyticsRecordsTerminalReceipts(t *testing.T) {
	for _, outcome := range []string{"won", "declined", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			store, _, id := duelStoreFor(t, map[string]int64{"alice": 100, "bob": 100})
			capture := analyticstest.New(t)
			store.SetAnalytics(capture.Recorder)
			_, err := store.Open(t.Context(), id, DuelOpenSpec{Kind: DuelChallenge, Opener: p("alice"), Challenged: p("bob"), Stake: 10})
			require.NoError(t, err)
			resolveDuel(t, store, id, outcome)
			resolveDuel(t, store, id, outcome)
			want := map[string]int64{"games.rounds|duel|" + outcome: 1, "games.entrants|duel|2_4": 1}
			require.Equal(t, want, capture.Counters())
		})
	}
}

func resolveDuel(t *testing.T, store *ValkeyDuelStore, id uint64, outcome string) {
	t.Helper()
	switch outcome {
	case "won":
		_, err := store.Accept(t.Context(), id, p("bob"))
		require.NoError(t, err)
	case "declined":
		_, err := store.Decline(t.Context(), id, p("bob"))
		require.NoError(t, err)
	default:
		_, err := store.Cancel(t.Context(), id, p("alice"), false)
		require.NoError(t, err)
	}
}

func TestLoyaltyWalletCountsSuccessfulDebits(t *testing.T) {
	capture := analyticstest.New(t)
	wallet := NewLoyaltyWallet(&recordingLoyalty{})
	wallet.SetAnalytics(capture.Recorder)
	_, _, err := wallet.Debit(t.Context(), 1, DuelStake{ViewerID: 2, Login: "private", Stake: 100})
	require.NoError(t, err)
	_, err = wallet.Credit(t.Context(), 1, DuelStake{ViewerID: 2, Login: "private", Stake: 100})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"points.flows|duel_stake": 1}, capture.Counters())
}

func TestRafflePayoutCountsSuccessfulWinnerCredits(t *testing.T) {
	wallet := &fakeRaffleWallet{}
	store, id := raffleStoreWith(t, RaffleConfig{AnnouncementConfig: AnnouncementConfig{Proj: loyaltyReader(true, "")}, Wallet: wallet})
	capture := analyticstest.New(t)
	store.SetAnalytics(capture.Recorder)
	openAndJoin(t, store, id, raffleSetup{RaffleState{Prize: 1000}, numberedEntrants(5)})
	_, err := store.Draw(t.Context(), id, 3)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"games.rounds|raffle|drawn": 1, "games.entrants|raffle|5_19": 1, "points.flows|raffle_payout": 3,
	}, capture.Counters())
}

func TestTrialAnalyticsRecordsOnlyCompletedPromotion(t *testing.T) {
	vc := localValkey(t)
	seedTrial(t, vc, seededTrial{id: "4242", state: "promoted"})
	capture := analyticstest.New(t)
	store := NewTrialPromotion(vc, &fakePromoter{}, zap.NewNop())
	store.SetAnalytics(capture.Recorder)
	store.Sweep(t.Context())
	store.Sweep(t.Context())
	require.Equal(t, map[string]int64{"lifecycle|trial_promoted": 1}, capture.Counters())
}

func TestTimerRefusalAnalyticsToleratesUnwiredPipeline(t *testing.T) {
	store := &ValkeyTimerStore{log: zap.NewNop(), now: time.Now}
	require.NotPanics(t, func() { store.recordTick("refused") })
	capture := analyticstest.New(t)
	store.WirePipeline(&Pipeline{analytics: capture.Recorder})
	store.recordTick("refused")
	store.recordTimerOutput(1, 0, false)
	require.Equal(t, map[string]int64{"timers.ticks|refused": 1}, capture.Counters())
}

func TestTimerRejectedFireAnalyticsRegression(t *testing.T) {
	for _, row := range []struct {
		result, message string
		refused         bool
	}{
		{"floor", "steal tokens via grabify.link now", false},
		{"blank", " \n\t", false}, {"refused", "hello", true},
	} {
		t.Run(row.result, func(t *testing.T) {
			capture := analyticstest.New(t)
			pipe := timerPipeline(nil, nil)
			defer pipe.Close()
			pipe.analytics = capture.Recorder
			store, _ := timerFireStore(pipe)
			if row.refused {
				store.runtime = announcementRuntime{channelruntime.Snapshot{}}
			}
			at := armedTimer{ref: timerRef{broadcasterID: 1, id: "t1"}, def: timerDef{Message: row.message}}
			store.fire(t.Context(), at)
			store.fire(t.Context(), at)
			require.Equal(t, map[string]int64{"timers.ticks|" + row.result: 2}, capture.Counters())
		})
	}
}

func TestModerationAnalyticsFromProcess(t *testing.T) {
	for _, row := range []struct {
		name   string
		shadow bool
		cohort int
		rule   string
	}{
		{"shadow", true, 0, "ip_logger"}, {"cohort shadow", true, 2, "ip_logger"},
		{"enforced", false, 0, "ip_logger"}, {"cohort enforced", false, 2, "ip_logger"},
	} {
		t.Run(row.name, func(t *testing.T) {
			run := (automodRig{shadow: row.shadow}).build()
			capture := analyticstest.New(t)
			run.p.analytics = capture.Recorder
			run.pub.failErr = fmt.Errorf("publication rejected")
			_ = run.p.Process((automodMsg{text: ipLogger.text, cohort: row.cohort}).message(t))
			run.p.Close()
			mode := "enforced"
			if row.shadow {
				mode = "shadow"
			}
			require.EqualValues(t, 1, capture.Counters()["automod.flags|"+row.rule+"|"+mode])
		})
	}
}
