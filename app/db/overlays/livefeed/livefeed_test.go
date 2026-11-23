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
	"ItsBagelBot/pkg/ratelimit"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type published struct {
	Subject string
	ID      string
	Seq     uint64
	Payload []byte
}

type fakeFrames struct {
	mu     sync.Mutex
	stored []published
	byID   map[string]uint64
}

func (f *fakeFrames) Publish(_ context.Context, subject, id string, payload []byte) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if seq, duplicate := f.byID[id]; duplicate {
		return seq, nil
	}
	seq := uint64(len(f.stored) + 1)
	f.stored = append(f.stored, published{Subject: subject, ID: id, Seq: seq, Payload: payload})
	f.byID[id] = seq
	return seq, nil
}

func (f *fakeFrames) subjects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.stored))
	for i, frame := range f.stored {
		out[i] = frame.Subject
	}
	return out
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
	frames   *fakeFrames
	channels *fakeChannels
	owner    string
	allow    bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		frames:   &fakeFrames{byID: map[string]uint64{}},
		channels: &fakeChannels{profile: repository.FeedProfile{Owned: true}},
		owner:    strconv.FormatInt(1_000_000+rand.Int64N(1<<40), 10),
		allow:    true,
	}
	h.service = &livefeed.Service{
		Client:   valkeytest.Real(t),
		Frames:   h.frames,
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

	require.Equal(t, []string{feed.Subject(h.owner, feed.FrameEvent), feed.Subject(h.owner, feed.FrameStats)}, h.frames.subjects())
	require.Equal(t, int64(150), snapshot.Snapshot.Stats.Session.Bits)
	require.Equal(t, int64(150), snapshot.Snapshot.Stats.Week.Bits)
	require.Equal(t, int64(150), h.channels.totals.Counts.Bits)
	require.Equal(t, uint64(2), snapshot.StreamSeq)
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

	require.Empty(t, h.frames.subjects())
	require.Zero(t, h.snapshot(t).Snapshot.Stats.Session.Bits)
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
	require.Len(t, h.frames.subjects(), 1)
}

func TestReplayRepublishesALoggedEvent(t *testing.T) {
	h := newHarness(t)
	cheer := h.event(channelevents.Cheer, 100)
	require.NoError(t, h.service.Handle(t.Context(), cheer))
	before := len(h.frames.subjects())

	_, replayed := h.control(overlaysrpc.FeedReplay, overlaysrpc.FeedControlArgs{EventID: cheer.ID})
	_, missing := h.control(overlaysrpc.FeedReplay, overlaysrpc.FeedControlArgs{EventID: "unknown"})

	require.NoError(t, replayed)
	require.ErrorIs(t, missing, overlaysrpc.ErrFeedUnknownEvent)
	require.Len(t, h.frames.subjects(), before+1)
}
