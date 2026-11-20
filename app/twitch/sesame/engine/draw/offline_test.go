// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/pkg/valkey"

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
		name          string
		terms         draw.Terms
		liveAtOpen    bool
		offlineOffset time.Duration
		pauses        bool
		manual        bool
	}{
		{name: "giveaway never live closes on time", terms: draw.Terms{Kind: draw.KindGiveaway}},
		{name: "giveaway live at open waits for live", terms: draw.Terms{Kind: draw.KindGiveaway}, liveAtOpen: true, pauses: true},
		{name: "giveaway going offline during entries waits for live", terms: draw.Terms{Kind: draw.KindGiveaway}, offlineOffset: time.Millisecond, pauses: true},
		{name: "mod can draw while offline", terms: draw.Terms{Kind: draw.KindGiveaway}, liveAtOpen: true, pauses: true, manual: true},
		{name: "raffle closes while offline", terms: draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 100}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := &drawStream{}
			beforeOpen := time.Now().Add(-time.Minute)
			live.set(tc.liveAtOpen, beforeOpen)
			f := newFixture(t, draw.Config{Live: live})
			r := f.openFor(429, tc.terms, time.Second)
			if tc.pauses {
				live.set(false, r.OpenedAt.Add(tc.offlineOffset))
				require.NoError(t, f.Wake(t.Context(), r.Channel))
			}
			f.start(f.Store)
			if tc.pauses {
				f.resumeOfflineGiveaway(r, live, tc.manual)
			}
			waitRemoved(t, f.client, drawKeys(r))
		})
	}
}

func (f fixture) resumeOfflineGiveaway(r draw.Round, live *drawStream, manual bool) {
	t := f.t
	t.Helper()
	index := valkey.DueIndex{Key: drawKeys(r).Due}
	require.Eventually(t, func() bool {
		if live.checks.Load() < 2 {
			return false
		}
		ids, _, err := index.Lapsed(t.Context(), f.client, 100)
		return err == nil && len(ids) == 0
	}, 4*time.Second, 25*time.Millisecond)
	round, joinable, err := f.Joinable(t.Context(), r.Channel)
	require.NoError(t, err)
	require.Equal(t, []any{draw.PhaseOpen, false}, []any{round.Phase, joinable})
	if manual {
		f.close(r.Channel)
		return
	}
	live.set(true, f.now())
	require.NoError(t, f.Wake(t.Context(), r.Channel))
}

func TestDrawOfflineGiveawayClosesAtSnapshotExpiry(t *testing.T) {
	live := &drawStream{}
	live.set(true, time.Now().Add(-time.Minute))
	f := newFixture(t, draw.Config{Live: live})
	now := f.now().Truncate(time.Millisecond)
	r := f.openRequest(draw.OpenRequest{
		Channel: 434, OpenedAt: now.Add(3*time.Second - draw.Pins.GiveawayMaxEntryWindow),
		Terms: draw.Terms{Kind: draw.KindGiveaway, Prize: draw.Prize{Item: "poster"}, SnapshotID: "snapshot", ClosesAt: now.Add(time.Second)},
	})
	live.set(false, f.now())
	f.start(f.Store)

	require.Eventually(t, func() bool {
		round, joinable, err := f.Joinable(t.Context(), r.Channel)
		return err == nil && round.Phase == draw.PhaseOpen && !joinable && live.checks.Load() >= 2
	}, 3*time.Second, 25*time.Millisecond)
	waitRemoved(t, f.client, drawKeys(r))
	require.False(t, f.join(r, "11").Open)
}

func TestDrawOfflineClaimClock(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pauses bool
		resume bool
	}{
		{"can claim during a pause", true, false},
		{"live return extends the claim deadline", true, true},
		{"giveaway never live runs the claim clock", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ann := make(notices, 5)
			live := &drawStream{}
			live.set(tc.pauses, time.Now().Add(-time.Minute))
			f := newFixture(t, draw.Config{Live: live, Announcer: ann})
			r := f.open(430, draw.Terms{Kind: draw.KindGiveaway, ClaimWindow: 30 * time.Second})
			f.join(r, "11")
			f.close(r.Channel)
			f.start(f.Store)
			slot := ann.next(t, draw.NoticeWinners).Slots[0]
			if tc.pauses {
				f.pauseClaim(r, slot, live, tc.resume)
				return
			}
			now := f.now()
			slot.Deadline = now.Add(100 * time.Millisecond)
			f.rewriteSlots(r, []draw.Slot{slot}, now)
			require.Equal(t, slot.Viewer, ann.next(t, draw.NoticeNoShow).Slots[0].Viewer)
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
	require.NoError(t, f.Wake(t.Context(), r.Channel))
	f.waitClaimClock(r.Channel, false)
	require.Eventually(t, func() bool { return !time.Now().Before(slot.Deadline) }, 2*time.Second, 20*time.Millisecond)
	if resume {
		live.set(true, f.now())
		require.NoError(t, f.Wake(t.Context(), r.Channel))
		f.waitClaimClock(r.Channel, true)
	}
	result, err := f.Claim(t.Context(), r.Channel, slot.Viewer)
	require.NoError(t, err)
	require.Equal(t, draw.ClaimOutcome{Claimed: 1}, result)
}

func (f fixture) waitClaimClock(ch draw.ChannelID, live bool) {
	f.t.Helper()
	require.Eventually(f.t, func() bool {
		round, _, ok, err := f.Settling(f.t.Context(), ch, draw.KindGiveaway)
		if err != nil || !ok {
			return false
		}
		return round.ClaimPausedAt.IsZero() == live
	}, 4*time.Second, 25*time.Millisecond)
}
