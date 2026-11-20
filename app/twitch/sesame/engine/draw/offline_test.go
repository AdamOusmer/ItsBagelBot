// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"

	"github.com/stretchr/testify/require"
)

type drawStream struct {
	state  atomic.Pointer[draw.LiveState]
	checks atomic.Int64
}

func (s *drawStream) DrawLiveState(context.Context, uint64) (draw.LiveState, error) {
	state := *s.state.Load()
	s.checks.Add(1)
	return state, nil
}

func (s *drawStream) set(live bool, at time.Time) {
	s.state.Store(&draw.LiveState{Live: live, ChangedAt: at})
}

func TestDrawOfflineEntryWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		terms  draw.Terms
		manual bool
	}{
		{"giveaway waits for live", draw.Terms{Kind: draw.KindGiveaway}, false},
		{"mod can draw while offline", draw.Terms{Kind: draw.KindGiveaway}, true},
		{"raffle closes while offline", draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 100}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := &drawStream{}
			live.set(false, time.Now())
			f := newFixture(t, draw.Config{Live: live})
			r := f.openFor(429, tc.terms, time.Second)
			f.start(f.Store)
			if tc.terms.Kind == draw.KindGiveaway {
				f.resumeOfflineGiveaway(r, live, tc.manual)
			}
			waitRemoved(t, f.client, drawKeys(r))
		})
	}
}

func (f fixture) resumeOfflineGiveaway(r draw.Round, live *drawStream, manual bool) {
	t := f.t
	t.Helper()
	require.Eventually(t, func() bool { return live.checks.Load() != 0 }, 4*time.Second, 25*time.Millisecond)
	round, joinable, err := f.Joinable(t.Context(), r.Channel)
	require.NoError(t, err)
	require.Equal(t, []any{draw.PhaseOpen, false}, []any{round.Phase, joinable})
	if manual {
		f.close(r.Channel)
		return
	}
	live.set(true, f.now())
	f.shiftFields(r, f.now(), testTimeField("closes_at", r.ClosesAt))
}

func TestDrawOfflineClaimClock(t *testing.T) {
	for _, tc := range []struct {
		name   string
		resume bool
	}{
		{"can claim during a pause", false},
		{"live return extends the claim deadline", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ann := make(notices, 5)
			live := &drawStream{}
			live.set(true, time.Now())
			f := newFixture(t, draw.Config{Live: live, Announcer: ann})
			r := f.open(430, draw.Terms{Kind: draw.KindGiveaway, ClaimWindow: 30 * time.Second})
			f.join(r, "11")
			f.close(r.Channel)
			f.start(f.Store)
			slot := ann.next(t, draw.NoticeWinners).Slots[0]
			f.pauseClaim(r, slot, live, tc.resume)
		})
	}
}

func (f fixture) pauseClaim(r draw.Round, slot draw.Slot, live *drawStream, resume bool) {
	t := f.t
	t.Helper()
	offlineAt := f.now().Truncate(time.Millisecond)
	live.set(false, offlineAt)
	slot.Deadline = offlineAt.Add(500 * time.Millisecond)
	f.rewriteSlots(r, []draw.Slot{slot}, offlineAt)
	f.waitClaimClock(r.Channel, false)
	require.Eventually(t, func() bool { return !time.Now().Before(slot.Deadline) }, 2*time.Second, 20*time.Millisecond)
	if resume {
		live.set(true, f.now())
	}
	result, err := f.Claim(t.Context(), r.Channel, slot.Viewer)
	require.NoError(t, err)
	require.Equal(t, draw.ClaimOutcome{Claimed: 1}, result)
}

func (f fixture) waitClaimClock(ch draw.ChannelID, live bool) {
	f.t.Helper()
	require.Eventually(f.t, func() bool {
		round, _, ok, err := f.Settling(f.t.Context(), ch, draw.KindGiveaway)
		return err == nil && ok && round.ClaimPausedAt.IsZero() == live
	}, 4*time.Second, 25*time.Millisecond)
}
