// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type verifyWith func(context.Context, draw.Check) (draw.Verdict, error)

func (v verifyWith) Verify(ctx context.Context, check draw.Check) (draw.Verdict, error) {
	return v(ctx, check)
}

type recordWith func(context.Context, draw.Round, []draw.Slot) error

func (r recordWith) Record(ctx context.Context, round draw.Round, slots []draw.Slot) error {
	return r(ctx, round, slots)
}

func TestDrawRetryAfterKeepsCandidateVerifying(t *testing.T) {
	const hint = 8 * time.Second
	f := newFixture(t, draw.Config{})
	round := f.open(429, draw.Terms{Kind: draw.KindGiveaway})
	f.join(round, "11")
	f.close(round.Channel)
	verified := make(chan time.Time, 1)
	cfg := testConfig(draw.Config{Verifier: verifyWith(func(ctx context.Context, _ draw.Check) (draw.Verdict, error) {
		time.Sleep(20 * time.Millisecond)
		at, err := valkey.ServerNow(ctx, f.client)
		if err != nil {
			return draw.Verdict{}, err
		}
		verified <- at
		return draw.Verdict{}, &engine.RetryAfterError{After: hint}
	})})
	f.start(draw.NewStore(f.client, cfg))
	slot := f.awaitSlot(round, func(slot draw.Slot) bool {
		return slot.State == draw.SlotVerifying && !slot.RetryAt.IsZero()
	})
	minRetry := take(t, verified).Add(hint)
	require.Equal(t, draw.ViewerID("11"), slot.Viewer)
	require.False(t, slot.RetryAt.Before(minRetry))
	f.awaitRetryDue(round, minRetry)
}

func (f fixture) awaitRetryDue(round draw.Round, minRetry time.Time) {
	f.t.Helper()
	minDue := minRetry.Add(time.Millisecond - time.Nanosecond).UnixMilli()
	require.EventuallyWithT(f.t, func(check *assert.CollectT) {
		ids, err := valkey.ZRangeByScore(f.t.Context(), f.client, drawKeys(round).Due, valkey.ScoreRange{
			Min: strconv.FormatInt(minDue, 10), Max: "+inf", Limit: -1,
		})
		require.NoError(check, err)
		require.Contains(check, ids, string(round.ID))
	}, time.Second, 10*time.Millisecond)
}

func TestDrawRejectedCandidateReturnsToPendingWithReason(t *testing.T) {
	seen := make(chan draw.Check, 2)
	pending := make(chan draw.Slot, 1)
	ann := make(notices, 4)
	detail := map[string]string{"required": "subscription"}
	verifier := verifyWith(func(_ context.Context, check draw.Check) (draw.Verdict, error) {
		seen <- check
		return draw.Verdict{Pass: check.Viewer == "12", Reason: "role", Detail: detail}, nil
	})
	f := newFixture(t, draw.Config{Verifier: verifier, Announcer: ann})
	r := f.open(411, draw.Terms{Kind: draw.KindGiveaway})
	f.join(r, "11")
	_, err := f.Join(t.Context(), draw.JoinRequest{Channel: r.Channel, Round: r.ID, Viewer: "12", Login: "viewer12", Weight: 210})
	require.NoError(t, err)
	f.close(r.Channel)
	var picks int
	cfg := testConfig(draw.Config{Verifier: verifier, Announcer: ann, Random: func(int64) int64 {
		picks++
		if picks == 2 {
			_, slots, _, err := f.Settling(t.Context(), r.Channel, draw.KindGiveaway)
			if err == nil && len(slots) == 1 {
				pending <- slots[0]
			}
		}
		return 0
	}})
	f.start(draw.NewStore(f.client, cfg))
	want := draw.Slot{N: 1, Generation: 1, State: draw.SlotPending,
		History: []draw.Candidate{{Viewer: "11", Generation: 1, Reason: "role", Detail: detail}}}
	require.Equal(t, want, take(t, pending))
	first, second := take(t, seen), take(t, seen)
	require.Equal(t, []draw.Entry{{Weight: 100}, {Weight: 210}}, []draw.Entry{first.Entry, second.Entry})
	require.Equal(t, draw.ViewerID("12"), ann.next(t, draw.NoticeWinners).Slots[0].Viewer)
	waitRemoved(t, f.client, drawKeys(r))
}

type stuckCase struct {
	kind   draw.Kind
	action string
	want   draw.SlotState
	viewer draw.ViewerID
}

func TestDrawStuckVerification(t *testing.T) {
	cases := []stuckCase{
		{draw.KindGiveaway, "skip_rule", draw.SlotWon, "11"},
		{draw.KindGiveaway, "reroll", draw.SlotWon, "12"},
		{draw.KindGiveaway, "timeout", draw.SlotEnded, "11"},
		{draw.KindRaffle, "skip_rule", draw.SlotPaid, "11"},
		{draw.KindRaffle, "reroll", draw.SlotPaid, "12"},
		{draw.KindRaffle, "timeout", draw.SlotUnpaid, "11"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind)+"/"+tc.action, tc.run)
	}
}

func (tc stuckCase) run(t *testing.T) {
	ann := make(notices, 8)
	recorded := make(chan []draw.Slot, 1)
	f := newFixture(t, draw.Config{
		Announcer: ann, Verifier: verifyWith(verifyFirstCandidate), Random: func(int64) int64 { return 0 },
		Winners: recordWith(func(_ context.Context, _ draw.Round, slots []draw.Slot) error {
			recorded <- slots
			return nil
		}),
	})
	terms := draw.Terms{Kind: tc.kind}
	if tc.kind == draw.KindRaffle {
		terms.Prize.Points = 50
	}
	r := f.open(412, terms)
	f.join(r, "11")
	f.join(r, "12")
	f.close(r.Channel)
	f.start(f.Store)
	tc.resolve(f, r)
	slot := tc.finishedSlot(t, ann, recorded)
	require.Equal(t, tc.want, slot.State)
	require.Equal(t, tc.viewer, slot.Viewer)
	waitRemoved(t, f.client, drawKeys(r))
}

func verifyFirstCandidate(_ context.Context, check draw.Check) (draw.Verdict, error) {
	if check.Viewer != "11" {
		return draw.Verdict{Pass: true}, nil
	}
	return draw.Verdict{}, fmt.Errorf("lookup: %w", &engine.RetryAfterError{After: 3 * time.Second})
}

func (tc stuckCase) resolve(f fixture, r draw.Round) {
	t := f.t
	t.Helper()
	slot := f.awaitSlot(r, func(s draw.Slot) bool { return s.State == draw.SlotVerifying && !s.RetryAt.IsZero() })
	require.InDelta(t, 3, slot.RetryAt.Sub(slot.Since).Seconds(), 0.2)
	f.checkHintScheduled(r)
	req := draw.SlotAction{Channel: r.Channel, Round: r.ID, Actor: "mod", Slot: slot.N}
	for _, action := range []func(context.Context, draw.SlotAction) (int, error){f.SkipRule, f.Reroll} {
		applied, err := action(t.Context(), req)
		require.NoError(t, err)
		require.Zero(t, applied)
	}
	tc.resolveStuck(f, r, slot, req)
}

func (tc stuckCase) resolveStuck(f fixture, r draw.Round, slot draw.Slot, req draw.SlotAction) {
	now := f.now()
	slot.Since = now.Add(-draw.Pins.VerifyStuck - time.Second)
	if tc.action == "timeout" {
		tc.expire(f, r, slot, now)
		return
	}
	f.rewriteSlots(r, []draw.Slot{slot}, now)
	action := map[string]func(context.Context, draw.SlotAction) (int, error){"skip_rule": f.SkipRule, "reroll": f.Reroll}[tc.action]
	got, err := action(f.t.Context(), req)
	require.NoError(f.t, err)
	require.Equal(f.t, 1, got)
}

func (tc stuckCase) expire(f fixture, r draw.Round, slot draw.Slot, now time.Time) {
	if tc.kind == draw.KindRaffle {
		f.ageRaffle(r)
		return
	}
	slot.Since = slot.Since.Add(-draw.Pins.ModActionTimeout)
	f.rewriteSlots(r, []draw.Slot{slot}, now)
}

func (tc stuckCase) finishedSlot(t *testing.T, ann notices, recorded <-chan []draw.Slot) draw.Slot {
	t.Helper()
	switch {
	case tc.kind == draw.KindGiveaway:
		return take(t, recorded)[0]
	case tc.action == "timeout":
		return ann.next(t, draw.NoticeUnpaid).Slots[0]
	default:
		return ann.next(t, draw.NoticeWinners).Slots[0]
	}
}

func (f fixture) checkHintScheduled(r draw.Round) {
	f.t.Helper()
	require.EventuallyWithT(f.t, func(check *assert.CollectT) {
		ids, next, err := (valkey.DueIndex{Key: "draw:due"}).Lapsed(f.t.Context(), f.client, 100)
		require.NoError(check, err)
		require.NotContains(check, ids, string(r.ID))
		require.Greater(check, next, 2*time.Second)
	}, time.Second, 10*time.Millisecond)
}

func (f fixture) awaitSlot(r draw.Round, matches func(draw.Slot) bool) draw.Slot {
	f.t.Helper()
	var slot draw.Slot
	require.EventuallyWithT(f.t, func(check *assert.CollectT) {
		_, slots, found, err := f.Settling(f.t.Context(), r.Channel, r.Terms.Kind)
		require.NoError(check, err)
		require.True(check, found)
		require.Len(check, slots, 1)
		slot = slots[0]
		require.True(check, matches(slot), "slot %+v", slot)
	}, 5*time.Second, 10*time.Millisecond)
	return slot
}

func TestDrawStockCreatesUnfilledSlots(t *testing.T) {
	for _, count := range []int{0, 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ann := make(notices, 8)
			f := newFixture(t, draw.Config{Announcer: ann})
			r := f.open(413, draw.Terms{Kind: draw.KindGiveaway, Winners: draw.WinnerPolicy{Count: 3}})
			for n := range count {
				f.join(r, draw.ViewerID(fmt.Sprint(n+11)))
			}
			f.close(r.Channel)
			f.start(f.Store)
			unfilled := ann.next(t, draw.NoticeUnfilled)
			require.Equal(t, 3, unfilled.Round.Winners)
			require.Len(t, unfilled.Slots, 3-count)
			require.Equal(t, count+1, unfilled.Slots[0].N)
			waitRemoved(t, f.client, drawKeys(r))
		})
	}
}

type recordAttempt struct {
	round draw.Round
	slots []draw.Slot
	phase draw.Phase
	err   error
}

func TestDrawRecordsBeforeDoneAndRetriesWrite(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("first write fails=%t", failFirst), func(t *testing.T) {
			attempts := make(chan recordAttempt, 2)
			var calls atomic.Int32
			var f fixture
			f = newFixture(t, draw.Config{Winners: recordWith(func(ctx context.Context, r draw.Round, slots []draw.Slot) error {
				raw, err := drawKeys(r).Metadata(ctx, f.client)
				attempts <- recordAttempt{round: r, slots: slots, phase: draw.Phase(raw["phase"]), err: err}
				if calls.Add(1) == 1 && failFirst {
					return errors.New("winner history unavailable")
				}
				return err
			})})
			r := f.open(414, draw.Terms{Kind: draw.KindGiveaway})
			f.join(r, "11")
			f.close(r.Channel)
			f.start(f.Store)
			first := take(t, attempts)
			require.NoError(t, first.err)
			require.Equal(t, draw.PhaseSettling, first.phase)
			require.Equal(t, draw.SlotWon, first.slots[0].State)
			if failFirst {
				require.Equal(t, first, take(t, attempts))
			}
			waitRemoved(t, f.client, drawKeys(r))
			require.Empty(t, attempts)
		})
	}
}

type payReplay struct {
	beforeCommit func(context.Context) error
	first        draw.Payment
	replies      chan paymentReply
}

type paymentReply struct {
	payment  draw.Payment
	replayed bool
}

func (p *payReplay) Instance(context.Context, draw.ChannelID, draw.ViewerID) (int64, error) {
	return 901, nil
}

func (p *payReplay) Pay(ctx context.Context, payment draw.Payment) error {
	replayed := p.first.Key != ""
	if !replayed {
		p.first = payment
	}
	p.replies <- paymentReply{payment, replayed}
	if replayed {
		return nil
	}
	return p.beforeCommit(ctx)
}

func TestDrawReplaysPaymentAfterLostSlotCommit(t *testing.T) {
	payer := &payReplay{replies: make(chan paymentReply, 2)}
	ann := make(notices, 4)
	f := newFixture(t, draw.Config{Payer: payer, Announcer: ann})
	r := f.open(415, draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 50}})
	payer.beforeCommit = func(ctx context.Context) error {
		now, err := valkey.ServerNow(ctx, f.client)
		if err != nil {
			return err
		}
		_, err = drawKeys(r).Shift(ctx, f.client, valkey.PhaseShift{
			From: "settling", To: "settling", RoundID: string(r.ID), TTL: time.Hour,
			Wake: valkey.DrawWake{At: now, Channel: "draw:due:wake"},
		})
		return err
	}
	f.join(r, "11")
	f.close(r.Channel)
	f.start(f.Store)
	first, replay := take(t, payer.replies), take(t, payer.replies)
	require.Equal(t, []bool{false, true}, []bool{first.replayed, replay.replayed})
	require.Equal(t, first.payment, replay.payment)
	require.Equal(t, draw.SlotPaid, ann.next(t, draw.NoticeWinners).Slots[0].State)
	waitRemoved(t, f.client, drawKeys(r))
}
