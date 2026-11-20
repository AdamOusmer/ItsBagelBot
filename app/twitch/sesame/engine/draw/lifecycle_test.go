// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

type phaseTrace chan draw.Round

func (p phaseTrace) LeftOpen(_ context.Context, r draw.Round) { p <- r }

func TestDrawCancelInOpenAndClosing(t *testing.T) {
	for _, phase := range []draw.Phase{draw.PhaseOpen, draw.PhaseClosing} {
		t.Run(string(phase), func(t *testing.T) {
			phases := make(phaseTrace, 2)
			f := newFixture(t, draw.Config{Phases: phases})
			r := f.open(421, draw.Terms{Kind: draw.KindGiveaway})
			if phase == draw.PhaseClosing {
				f.close(r.Channel)
			}
			req := draw.ModAction{Channel: r.Channel, Actor: "mod", Round: r.ID}
			for _, want := range []bool{true, false} {
				got, err := f.Cancel(t.Context(), req)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
			require.Equal(t, r, take(t, phases))
			require.Empty(t, phases)
			f.start(f.Store)
			waitRemoved(t, f.client, drawKeys(r))
		})
	}
}

func TestDrawLateClaimAndNoShowTimeout(t *testing.T) {
	ann, records := make(notices, 4), make(chan []draw.Slot, 1)
	f := newFixture(t, draw.Config{Announcer: ann, Winners: captureSlots(records)})
	r := f.open(422, draw.Terms{Kind: draw.KindGiveaway, ClaimWindow: draw.Pins.ClaimWindowMin})
	f.join(r, "11")
	f.close(r.Channel)
	f.start(f.Store)
	winner := ann.next(t, draw.NoticeWinners).Slots[0]
	now := f.now()
	winner.Deadline = now.Add(-time.Second)
	f.rewriteSlots(r, []draw.Slot{winner}, now.Add(time.Second))
	claim, err := f.Claim(t.Context(), r.Channel, winner.Viewer)
	require.NoError(t, err)
	require.Equal(t, draw.ClaimOutcome{Late: 1}, claim)
	missed := ann.next(t, draw.NoticeNoShow).Slots[0]
	now = f.now()
	missed.Deadline = now.Add(-draw.Pins.ModActionTimeout - time.Second)
	f.rewriteSlots(r, []draw.Slot{missed}, now)
	require.Equal(t, draw.SlotEnded, take(t, records)[0].State)
	waitRemoved(t, f.client, drawKeys(r))
}

func captureSlots(records chan<- []draw.Slot) draw.WinnerSink {
	return recordWith(func(_ context.Context, _ draw.Round, slots []draw.Slot) error {
		records <- slots
		return nil
	})
}

func TestDrawEndSlotsKeepsClaimsAndDropsUnclaimedRefs(t *testing.T) {
	ann, records := make(notices, 4), make(chan []draw.Slot, 1)
	f := newFixture(t, draw.Config{Announcer: ann, Winners: captureSlots(records)})
	r := f.open(423, draw.Terms{
		Kind: draw.KindGiveaway, ClaimWindow: draw.Pins.ClaimWindowMin, Winners: draw.WinnerPolicy{Count: 2},
	})
	f.join(r, "11")
	f.join(r, "12")
	f.close(r.Channel)
	f.start(f.Store)
	slots := ann.next(t, draw.NoticeWinners).Slots
	claimed, err := f.Claim(t.Context(), r.Channel, slots[0].Viewer)
	require.NoError(t, err)
	require.Equal(t, draw.ClaimOutcome{Claimed: 1}, claimed)
	ended, err := f.EndSlots(t.Context(), draw.SlotAction{Channel: r.Channel, Actor: "mod", Round: r.ID})
	require.NoError(t, err)
	require.Equal(t, 1, ended)
	final := take(t, records)
	require.Equal(t, []draw.SlotState{draw.SlotClaimed, draw.SlotEnded}, []draw.SlotState{final[0].State, final[1].State})
	found, err := f.CanClaim(t.Context(), r.Channel, slots[1].Viewer)
	require.NoError(t, err)
	require.False(t, found)
	waitRemoved(t, f.client, drawKeys(r))
}

func TestDrawExplicitIDAndLiveRefusalCount(t *testing.T) {
	f := newFixture(t, draw.Config{})
	ctx := t.Context()
	id := draw.NewRoundID()
	terms := draw.Terms{Kind: draw.KindGiveaway, ClosesAt: f.now().Add(time.Minute)}
	req := draw.OpenRequest{ID: id, Channel: 425, Terms: terms}
	r, code, err := f.Open(ctx, req)
	require.NoError(t, err)
	require.Equal(t, draw.Opened, code)
	require.Equal(t, id, r.ID)
	keys := drawKeys(r)
	t.Cleanup(func() { require.NoError(t, keys.Teardown(context.Background(), f.client, string(id))) })
	f.join(r, "11")
	first, count, err := keys.Refuse(ctx, f.client, valkey.Refusal{Viewer: "12", Reason: "role", TTL: time.Hour})
	require.NoError(t, err)
	require.True(t, first)
	require.EqualValues(t, 1, count)
	f.close(r.Channel)
	next := f.open(r.Channel, terms)
	views, err := f.Live(ctx, r.Channel)
	require.NoError(t, err)
	require.Len(t, views, 2)
	require.Equal(t, draw.RoundView{Round: next, Slots: []draw.Slot{}}, views[0])
	require.Equal(t, draw.RoundView{Round: views[1].Round, Slots: []draw.Slot{}, Entries: 1, Refused: 1}, views[1])
	require.Equal(t, r.ID, views[1].ID)
	req.Channel++
	_, code, err = f.Open(ctx, req)
	require.NoError(t, err)
	require.Equal(t, draw.JoinableBusy, code)
}

func TestDrawOpenReplayReturnsStoredRound(t *testing.T) {
	f := newFixture(t, draw.Config{})
	r := f.open(431, draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "poster"}})
	req := draw.OpenRequest{ID: r.ID, Channel: r.Channel, Terms: r.Terms}
	for _, tc := range []struct {
		name  string
		phase draw.Phase
		setup func()
	}{
		{"open", draw.PhaseOpen, func() {}},
		{"closing alongside a newer open round", draw.PhaseClosing, func() {
			f.close(r.Channel)
			f.open(r.Channel, r.Terms)
		}},
		{"retry with elapsed deadline", draw.PhaseClosing, func() { req.Terms.ClosesAt = r.OpenedAt }},
	} {
		tc.setup()
		got, code, err := f.Open(t.Context(), req)
		require.NoError(t, err, tc.name)
		type replay struct {
			code  draw.OpenOutcome
			id    draw.RoundID
			phase draw.Phase
			terms draw.Terms
		}
		require.Equal(t, replay{draw.AlreadyOpened, r.ID, tc.phase, r.Terms}, replay{code, got.ID, got.Phase, got.Terms}, tc.name)
	}
}

func TestDrawJoinableReportsNewestActiveRound(t *testing.T) {
	f := newFixture(t, draw.Config{})
	older := f.open(432, draw.Terms{Kind: draw.KindGiveaway})
	f.close(older.Channel)
	r := f.open(older.Channel, older.Terms)
	for _, tc := range []struct {
		phase draw.Phase
		enter func()
	}{
		{draw.PhaseOpen, func() {}},
		{draw.PhaseClosing, func() { f.close(r.Channel) }},
		{draw.PhaseSettling, func() {
			ok, err := drawKeys(r).Shift(t.Context(), f.client, valkey.PhaseShift{
				From: "closing", To: "settling", RoundID: string(r.ID), TTL: time.Hour,
			})
			require.NoError(t, err)
			require.True(t, ok)
		}},
	} {
		tc.enter()
		got, found, err := f.Joinable(t.Context(), r.Channel)
		require.NoError(t, err)
		type active struct {
			id    draw.RoundID
			phase draw.Phase
			open  bool
		}
		require.Equal(t, active{r.ID, tc.phase, tc.phase == draw.PhaseOpen}, active{got.ID, got.Phase, found})
	}
}

func (f fixture) shiftFields(r draw.Round, at time.Time, fields ...valkey.FieldValue) {
	f.t.Helper()
	applied, err := drawKeys(r).Shift(f.t.Context(), f.client, valkey.PhaseShift{
		From: string(r.Phase), To: string(r.Phase), RoundID: string(r.ID), TTL: time.Hour, Fields: fields,
		Wake: valkey.DrawWake{At: at, Channel: "draw:due:wake"},
	})
	require.NoError(f.t, err)
	require.True(f.t, applied)
}

func testTimeField(field string, at time.Time) valkey.FieldValue {
	return valkey.FieldValue{Field: field, Value: strconv.FormatInt(at.UnixMilli(), 10)}
}

func TestDrawOpenReminderIncludesTimeAndEntries(t *testing.T) {
	cases := []reminderCase{
		{"raffle", draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 50}}, true},
		{"giveaway opted in", draw.Terms{Kind: draw.KindGiveaway, RemindEnabled: true}, true},
		{"giveaway default", draw.Terms{Kind: draw.KindGiveaway}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

type reminderCase struct {
	name  string
	terms draw.Terms
	on    bool
}

func (tc reminderCase) run(t *testing.T) {
	ann := make(notices, 4)
	phases := make(phaseTrace, 2)
	f := newFixture(t, draw.Config{Announcer: ann, Phases: phases})
	r := f.openFor(426, tc.terms, 12*time.Minute)
	raw, err := drawKeys(r).Metadata(t.Context(), f.client)
	require.NoError(t, err)
	require.Equal(t, tc.on, raw["remind_at"] != "")
	f.join(r, "11")
	now := f.now()
	f.shiftFields(r, now, testTimeField("remind_at", now.Add(-time.Second)))
	applied, err := f.Extend(t.Context(), draw.ExtendRequest{Channel: r.Channel, Actor: "mod"})
	require.NoError(t, err)
	require.True(t, applied)
	r, _, err = f.Joinable(t.Context(), r.Channel)
	require.NoError(t, err)
	f.start(f.Store)
	if tc.on {
		f.checkReminder(r, ann, now)
	}
	f.checkHintScheduled(r)
	f.shiftFields(r, f.now(), testTimeField("closes_at", now))
	require.Equal(t, r.ID, take(t, phases).ID)
	waitRemoved(t, f.client, drawKeys(r))
	require.Empty(t, phases)
	for len(ann) > 0 {
		require.NotEqual(t, draw.NoticeRemind, (<-ann).Event)
	}
}

func (f fixture) checkReminder(r draw.Round, ann notices, now time.Time) {
	t := f.t
	t.Helper()
	got := ann.next(t, draw.NoticeRemind)
	require.Equal(t, draw.Notice{Event: draw.NoticeRemind, Round: r, Entries: 1, TimeLeft: got.TimeLeft}, got)
	require.InDelta(t, r.ClosesAt.Sub(now).Seconds(), got.TimeLeft.Seconds(), 0.3)
	f.checkNextReminder(r, now.Add(draw.Pins.RemindInterval))
}

func (f fixture) checkNextReminder(r draw.Round, at time.Time) {
	t := f.t
	t.Helper()
	raw, err := drawKeys(r).Metadata(t.Context(), f.client)
	require.NoError(t, err)
	next, err := strconv.ParseInt(raw["remind_at"], 10, 64)
	require.NoError(t, err)
	require.InDelta(t, at.UnixMilli(), next, 300)
}

func TestDrawVerifyingNoticeStartsWithRerollBatch(t *testing.T) {
	ann := make(notices, 8)
	f := newFixture(t, draw.Config{
		Announcer: ann, Random: func(int64) int64 { return 0 },
		Verifier: verifyWith(func(ctx context.Context, c draw.Check) (draw.Verdict, error) {
			if c.Viewer == "11" {
				return draw.Verdict{Pass: true}, nil
			}
			return draw.Verdict{}, &engine.RetryAfterError{After: time.Minute}
		}),
	})
	r := f.open(427, draw.Terms{Kind: draw.KindGiveaway, ClaimWindow: draw.Pins.ClaimWindowMin})
	f.join(r, "11")
	f.join(r, "12")
	f.close(r.Channel)
	f.start(f.Store)
	first := ann.next(t, draw.NoticeWinners).Slots
	f.expireLine(r, first)
	ann.next(t, draw.NoticeNoShow)
	r.Phase = draw.PhaseSettling
	now := f.now()
	f.shiftFields(r, now, testTimeField("settled_at", now.Add(-time.Minute)))
	applied, err := f.Reroll(t.Context(), draw.SlotAction{Channel: r.Channel, Round: r.ID})
	require.NoError(t, err)
	require.Equal(t, 1, applied)
	f.awaitSlot(r, func(slot draw.Slot) bool { return !slot.RetryAt.IsZero() })
	require.Empty(t, ann)
	f.shiftFields(r, now, testTimeField("working_since", now.Add(-draw.Pins.VerifyingNotice-time.Second)))
	require.Equal(t, r.ID, ann.next(t, draw.NoticeVerifying).Round.ID)
}

func TestDrawSweepAdvancesOtherRoundDuringVerification(t *testing.T) {
	blocked := make(chan struct{}, 1)
	release := make(chan struct{})
	ann := make(notices, 6)
	f := newFixture(t, draw.Config{Announcer: ann, Verifier: verifyWith(func(ctx context.Context, check draw.Check) (draw.Verdict, error) {
		if check.Viewer != "11" {
			return draw.Verdict{Pass: true}, nil
		}
		blocked <- struct{}{}
		select {
		case <-release:
			return draw.Verdict{Pass: true}, nil
		case <-ctx.Done():
			return draw.Verdict{}, ctx.Err()
		}
	})})
	first := f.open(428, draw.Terms{Kind: draw.KindGiveaway})
	second := f.open(429, draw.Terms{Kind: draw.KindGiveaway})
	f.join(first, "11")
	f.join(second, "12")
	f.close(first.Channel)
	f.close(second.Channel)
	f.start(f.Store)
	take(t, blocked)
	require.Equal(t, second.ID, ann.next(t, draw.NoticeWinners).Round.ID)
	close(release)
	require.Equal(t, first.ID, ann.next(t, draw.NoticeWinners).Round.ID)
}

func TestDrawCancelledWorkReleasesLease(t *testing.T) {
	started := make(chan struct{}, 1)
	f := newFixture(t, draw.Config{Verifier: verifyWith(func(ctx context.Context, _ draw.Check) (draw.Verdict, error) {
		started <- struct{}{}
		<-ctx.Done()
		return draw.Verdict{}, ctx.Err()
	})})
	r := f.open(430, draw.Terms{Kind: draw.KindGiveaway})
	f.join(r, "11")
	f.close(r.Channel)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	take(t, started)
	cancel()
	take(t, done)
	_, held, err := valkey.ReadString(t.Context(), f.client, drawKeys(r).Lease)
	require.NoError(t, err)
	require.False(t, held)
}
