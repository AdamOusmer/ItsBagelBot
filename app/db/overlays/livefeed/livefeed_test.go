// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed_test

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/livefeed"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/overlays/feed"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeBus struct {
	mu       sync.Mutex
	alerts   []bus.OverlayMessage
	ids      map[string]bool
	states   [][]byte
	subjects []string
	premium  bool
	alpha    bool
}

func (f *fakeBus) Publish(_ context.Context, msg bus.OverlayMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.ids[msg.EventID] {
		f.ids[msg.EventID] = true
		f.alerts = append(f.alerts, msg)
	}
	return nil
}

type fakeState struct{ bus *fakeBus }

func (f fakeState) Publish(_ context.Context, subject string, payload []byte) error {
	f.bus.mu.Lock()
	defer f.bus.mu.Unlock()
	f.bus.subjects = append(f.bus.subjects, subject)
	f.bus.states = append(f.bus.states, payload)
	return nil
}

func (f *fakeBus) Premium(context.Context, uint64) (bool, error) { return f.premium, nil }

func (f *fakeBus) Alpha(context.Context, uint64) (bool, error) { return f.alpha, nil }

func (f *fakeBus) alertCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.alerts)
}

func (f *fakeBus) stateTypes(t *testing.T) []feed.Frame {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]feed.Frame, len(f.states))
	for i, payload := range f.states {
		var frame overlaysrpc.StateFrame
		require.NoError(t, codec.Unmarshal(payload, &frame))
		types[i] = frame.Type
	}
	return types
}

type fakeChannels struct {
	profile repository.FeedProfile
	totals  repository.FeedTotals
}

func (c *fakeChannels) FeedProfile(context.Context, uint64) (repository.FeedProfile, error) {
	return c.profile, nil
}

func (c *fakeChannels) AddFeedTotals(_ context.Context, _ uint64, delta feed.Delta) error {
	for _, count := range delta.Counts {
		c.totals.Counts.Add(count)
	}
	return nil
}

func (c *fakeChannels) FeedTotals(context.Context, uint64) (repository.FeedTotals, error) {
	return c.totals, nil
}

type harness struct {
	service  *livefeed.Service
	bus      *fakeBus
	channels *fakeChannels
	owner    string
	allow    bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		bus:      &fakeBus{ids: map[string]bool{}, alpha: true},
		channels: &fakeChannels{profile: repository.FeedProfile{Owned: true}},
		owner:    strconv.FormatInt(1_000_000+rand.Int64N(1<<40), 10),
		allow:    true,
	}
	h.service = &livefeed.Service{
		Client:   valkeytest.Real(t),
		Alerts:   h.bus,
		State:    fakeState{bus: h.bus},
		Plans:    h.bus,
		Alphas:   h.bus,
		Channels: h.channels,
		Allow:    func(context.Context, ratelimit.Request) (bool, error) { return h.allow, nil },
		Log:      zap.NewNop(),
	}
	return h
}

func (h *harness) event(kind channelevents.Kind, amount int64) channelevents.Event {
	return channelevents.Event{
		ID: string(kind) + strconv.FormatInt(rand.Int64(), 36), Kind: kind, ChannelID: h.owner, OccurredAt: time.Now().UTC(), Amount: amount,
		User: &channelevents.User{ID: "77", Login: "viewer", Name: "Viewer"},
	}
}

func (h *harness) snapshot(t *testing.T) overlaysrpc.FeedSnapshotReply {
	t.Helper()
	reply, err := h.service.Snapshot(t.Context(), h.owner)
	require.NoError(t, err)
	return reply
}

func (h *harness) control(action overlaysrpc.FeedAction, args overlaysrpc.FeedControlArgs) (overlaysrpc.FeedControlReply, error) {
	return h.service.Control(context.Background(), overlaysrpc.FeedControlRequest{UserID: h.owner, Actor: h.owner, Action: action, Args: args})
}

func TestHandleFoldsEachEventOnceAndPublishesFrames(t *testing.T) {
	h := newHarness(t)
	cheer := h.event(channelevents.Cheer, 150)

	require.NoError(t, h.service.Handle(t.Context(), cheer))
	require.NoError(t, h.service.Handle(t.Context(), cheer))
	snapshot := h.snapshot(t)

	require.Equal(t, 1, h.bus.alertCount())
	require.Equal(t, []feed.Frame{feed.FrameStats, feed.FrameStats}, h.bus.stateTypes(t))
	require.Equal(t, []string{overlaysrpc.StateSubject(h.owner), overlaysrpc.StateSubject(h.owner)}, h.bus.subjects)
	require.Equal(t, int64(150), snapshot.Snapshot.Stats.Session.Bits)
	require.Equal(t, int64(150), snapshot.Snapshot.Stats.Week.Bits)
	require.Equal(t, int64(150), h.channels.totals.Counts.Bits)
}

func TestSnapshotCarriesLeadersLatestAndRecentEvents(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Cheer, 100)))
	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Cheer, 50)))

	stats := h.snapshot(t).Snapshot

	require.Equal(t, feed.Leader{User: feed.User{ID: "77", Login: "viewer", Name: "Viewer"}, Amount: 150}, *stats.Stats.TopCheerer)
	require.Contains(t, stats.Stats.Latest, feed.Cheer)
	require.Len(t, stats.Events, 2)
	require.Equal(t, int64(150), stats.Stats.Credits[feed.Cheer][0].Amount)
}

func TestChannelsWithoutAnOverlayAreIgnored(t *testing.T) {
	h := newHarness(t)
	h.channels.profile.Owned = false

	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Cheer, 100)))

	require.Zero(t, h.bus.alertCount())
	require.Empty(t, h.bus.states)
	require.Zero(t, h.snapshot(t).Snapshot.Stats.Session.Bits)
}

func TestChannelsWithoutAlphaAreIgnoredAndRefused(t *testing.T) {
	h := newHarness(t)
	h.bus.alpha = false

	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Cheer, 100)))
	_, controlErr := h.control(overlaysrpc.FeedPause, overlaysrpc.FeedControlArgs{})
	_, snapshotErr := h.service.Snapshot(t.Context(), h.owner)

	require.Zero(t, h.bus.alertCount())
	require.Empty(t, h.bus.states)
	require.ErrorIs(t, controlErr, overlaysrpc.ErrFeedAlphaRequired)
	require.ErrorIs(t, snapshotErr, overlaysrpc.ErrFeedAlphaRequired)
}

func TestRevokedOwnerStopsGettingFramesOnceTheAlphaCacheLapses(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Cheer, 100)))
	alertsWhileGranted, framesWhileGranted := h.bus.alertCount(), len(h.bus.stateTypes(t))

	h.bus.alpha = false
	require.NoError(t, valkey.NewKV(h.service.Client).Del(t.Context(), "ovf:alpha:"+h.owner))
	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Cheer, 100)))
	_, controlErr := h.control(overlaysrpc.FeedPause, overlaysrpc.FeedControlArgs{})
	_, snapshotErr := h.service.Snapshot(t.Context(), h.owner)

	require.NotZero(t, alertsWhileGranted)
	require.Equal(t, alertsWhileGranted, h.bus.alertCount())
	require.Len(t, h.bus.stateTypes(t), framesWhileGranted)
	require.ErrorIs(t, controlErr, overlaysrpc.ErrFeedAlphaRequired)
	require.ErrorIs(t, snapshotErr, overlaysrpc.ErrFeedAlphaRequired)
}

func TestSessionStartClearsSessionCountsAndKeepsWeek(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Cheer, 100)))
	started := h.event(channelevents.Session, 0)
	started.OccurredAt = time.Now().UTC().Add(time.Second)
	started.Change = &channelevents.SessionChange{State: channelevents.SessionStarted}

	require.NoError(t, h.service.Handle(t.Context(), started))
	stats := h.snapshot(t).Snapshot.Stats

	require.Equal(t, [2]int64{0, 100}, [2]int64{stats.Session.Bits, stats.Week.Bits})
}

func TestEarnedSubathonTimeAddsToTheClock(t *testing.T) {
	h := newHarness(t)
	h.channels.profile.Subathon = feed.SubathonRules{BaseSeconds: 600, FollowSeconds: 10}
	_, err := h.control(overlaysrpc.FeedSubathonReset, overlaysrpc.FeedControlArgs{})
	require.NoError(t, err)

	require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Follow, 0)))

	require.Equal(t, int64(610), h.snapshot(t).Snapshot.Subathon.RemainingSeconds)
}

func TestControlTogglesStateAndRefusesOtherActors(t *testing.T) {
	h := newHarness(t)

	paused, err := h.control(overlaysrpc.FeedPause, overlaysrpc.FeedControlArgs{})
	require.NoError(t, err)
	skipped, err := h.control(overlaysrpc.FeedSkip, overlaysrpc.FeedControlArgs{})
	require.NoError(t, err)
	_, refused := h.service.Control(t.Context(), overlaysrpc.FeedControlRequest{UserID: h.owner, Actor: "1", Action: overlaysrpc.FeedMute})

	require.True(t, paused.Control.Paused)
	require.Equal(t, feed.Control{Paused: true, Skip: 1}, skipped.Control)
	require.ErrorIs(t, refused, overlaysrpc.ErrFeedNotOwner)
}

func TestTestEventsAreValidatedThrottledAndFlagged(t *testing.T) {
	h := newHarness(t)
	args := overlaysrpc.FeedControlArgs{Kind: feed.Follow, Fields: &feed.Fields{User: &feed.User{ID: "5", Name: "Tester"}}}

	_, ok := h.control(overlaysrpc.FeedTest, args)
	_, badKind := h.control(overlaysrpc.FeedTest, overlaysrpc.FeedControlArgs{Kind: "nope"})
	h.allow = false
	_, throttled := h.control(overlaysrpc.FeedTest, args)

	require.NoError(t, ok)
	require.ErrorIs(t, badKind, overlaysrpc.ErrFeedInvalidArgs)
	require.ErrorIs(t, throttled, overlaysrpc.ErrFeedThrottled)
	require.Equal(t, 1, h.bus.alertCount())
}

func TestReplayRepublishesALoggedEvent(t *testing.T) {
	h := newHarness(t)
	cheer := h.event(channelevents.Cheer, 100)
	require.NoError(t, h.service.Handle(t.Context(), cheer))
	before := h.bus.alertCount()

	_, replayed := h.control(overlaysrpc.FeedReplay, overlaysrpc.FeedControlArgs{EventID: cheer.ID})
	_, missing := h.control(overlaysrpc.FeedReplay, overlaysrpc.FeedControlArgs{EventID: "unknown"})

	require.NoError(t, replayed)
	require.ErrorIs(t, missing, overlaysrpc.ErrFeedUnknownEvent)
	require.Equal(t, before+1, h.bus.alertCount())
}

func TestAlertsCarryTheOwnersTierKindAndExpiry(t *testing.T) {
	cases := []struct {
		name    string
		premium bool
		tier    bus.OverlayTier
	}{{"premium owner", true, bus.OverlayPremium}, {"free owner", false, bus.OverlayStandard}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.bus.premium = tc.premium
			cheer := h.event(channelevents.Cheer, 100)

			require.NoError(t, h.service.Handle(t.Context(), cheer))

			msg := h.bus.alerts[0]
			body, err := codec.Marshal(msg.Value)
			require.NoError(t, err)
			var wire map[string]any
			require.NoError(t, codec.Unmarshal(body, &wire))
			require.Equal(t, [4]any{tc.tier, bus.OverlayKindCustom, h.owner, cheer.ID}, [4]any{msg.Tier, msg.Kind, msg.UserID, msg.EventID})
			require.Equal(t, [2]any{cheer.ID, cheer.ID}, [2]any{wire["event_id"], wire["id"]})
			require.NotEmpty(t, wire["expires_at"])
		})
	}
}

func TestSnapshotKeepsEverySessionEvent(t *testing.T) {
	h := newHarness(t)
	const logged = 120
	for range logged {
		require.NoError(t, h.service.Handle(t.Context(), h.event(channelevents.Follow, 0)))
	}

	require.Len(t, h.snapshot(t).Snapshot.Events, logged)
}
