// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type giveawayLifecycleProjection struct {
	projection.Reader
}

func (giveawayLifecycleProjection) Module(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	raw := codec.RawMessage(`{"defaultPrize":"Game","windowSeconds":600,"defaultPreset":"all","presets":[{"name":"all","freezeWhileOpen":false,"rules":{"min_points":100}}]}`)
	return projection.ModuleView{Name: engine.GiveawayModuleName, IsEnabled: true, Configs: raw}, true, nil
}

type giveawayLifecycleClock struct{ engine.LoyaltyTicker }

func (giveawayLifecycleClock) AccrualHealth(context.Context, uint64) engine.AccrualState {
	return engine.AccrualHealthy
}

type giveawayLifecycleSnapshots struct {
	created engine.SnapshotRequest
	dropped chan droppedSnapshot
}

type droppedSnapshot struct {
	channel uint64
	id      string
}

func (s *giveawayLifecycleSnapshots) SnapshotCreate(_ context.Context, req engine.SnapshotRequest) (engine.SnapshotRef, error) {
	s.created = req
	return engine.SnapshotRef{SnapshotID: "snapshot"}, nil
}

func (s *giveawayLifecycleSnapshots) SnapshotDrop(_ context.Context, channel uint64, id string) error {
	s.dropped <- droppedSnapshot{channel, id}
	return nil
}

type giveawayLifecycleCase struct {
	name   string
	action func(*draw.Store, context.Context, draw.Round) (bool, error)
	window time.Duration
}

func TestGiveawaySnapshotLifecycle(t *testing.T) {
	for _, tc := range []giveawayLifecycleCase{
		{name: "manual close", action: closeSnapshotGiveaway, window: 30 * time.Second},
		{name: "cancel", action: cancelSnapshotGiveaway, window: 45 * time.Minute},
		{name: "timer", window: 2 * time.Second},
	} {
		t.Run(tc.name, tc.run)
	}
}

func (tc giveawayLifecycleCase) run(t *testing.T) {
	snapshots := &giveawayLifecycleSnapshots{dropped: make(chan droppedSnapshot, 2)}
	f := newFixture(t, draw.Config{Phases: engine.GiveawayPhaseObserver{Snapshots: snapshots, Log: zap.NewNop()}})
	deps := engine.Deps{Draw: f.Store, StandingsSnapshots: snapshots, Proj: giveawayLifecycleProjection{}, LoyaltyTick: giveawayLifecycleClock{}, Automod: automod.New()}
	outcome, err := engine.StartGiveaway(t.Context(), deps, engine.GiveawayStart{Channel: 42, Actor: "7", Window: tc.window})
	require.NoError(t, err)

	require.Empty(t, outcome.Refusal)
	left := i18n.CompactDuration("en", outcome.Round.ClosesAt.Sub(outcome.Round.OpenedAt))
	require.Contains(t, outcome.OpenedLine("en"), "Time: "+left+".")
	tc.checkRound(f, outcome.Round, snapshots)
}

func (tc giveawayLifecycleCase) checkRound(f fixture, r draw.Round, snapshots *giveawayLifecycleSnapshots) {
	f.t.Helper()
	ctx := f.t.Context()
	require.Equal(f.t, engine.SnapshotRequest{BroadcasterID: 42, Owner: "round:" + string(r.ID), ExpiresAt: r.ClosesAt}, snapshots.created)
	require.Equal(f.t, "snapshot", r.Terms.SnapshotID)
	f.t.Cleanup(func() {
		require.NoError(f.t, drawKeys(r).Teardown(context.Background(), f.client, string(r.ID)))
	})
	f.start(f.Store)
	if tc.action != nil {
		tc.applyAction(f, ctx, r)
	}
	require.Equal(f.t, droppedSnapshot{42, "snapshot"}, take(f.t, snapshots.dropped))
	require.Empty(f.t, snapshots.dropped)
}

func closeSnapshotGiveaway(s *draw.Store, ctx context.Context, r draw.Round) (bool, error) {
	return s.Close(ctx, draw.CloseRequest{Channel: r.Channel, Round: r.ID, Kind: draw.KindGiveaway})
}

func cancelSnapshotGiveaway(s *draw.Store, ctx context.Context, r draw.Round) (bool, error) {
	return s.Cancel(ctx, draw.ModAction{Channel: r.Channel, Round: r.ID, Kind: draw.KindGiveaway})
}

func (tc giveawayLifecycleCase) applyAction(f fixture, ctx context.Context, r draw.Round) {
	f.t.Helper()
	applied, err := tc.action(f.Store, ctx, r)
	require.NoError(f.t, err)
	require.True(f.t, applied)
}

func TestGiveawayRoundTargetRejectsStaleDashboardActions(t *testing.T) {
	f := newFixture(t, draw.Config{})
	prior := f.open(42, draw.Terms{Kind: draw.KindGiveaway})
	closed, err := closeSnapshotGiveaway(f.Store, t.Context(), prior)
	require.NoError(t, err)
	require.True(t, closed)
	current := f.open(prior.Channel, prior.Terms)
	actions := []func() (bool, error){
		func() (bool, error) { return closeSnapshotGiveaway(f.Store, t.Context(), prior) },
		func() (bool, error) {
			return f.Extend(t.Context(), draw.ExtendRequest{Channel: prior.Channel, Round: prior.ID, Kind: draw.KindGiveaway})
		},
	}
	for _, action := range actions {
		changed, err := action()
		require.NoError(t, err)
		require.False(t, changed)
	}
	matched, found, err := f.Joinable(t.Context(), prior.Channel)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, current.ID, matched.ID)
}

func TestDrawRerollsCurrentWinnerByLogin(t *testing.T) {
	ann := make(notices, 6)
	f := newFixture(t, draw.Config{Announcer: ann, Random: func(int64) int64 { return 0 }})
	r := f.open(431, draw.Terms{Kind: draw.KindGiveaway, ClaimWindow: 30 * time.Second})
	f.join(r, "11")
	f.join(r, "12")
	f.close(r.Channel)
	f.start(f.Store)
	winners := ann.next(t, draw.NoticeWinners).Slots
	f.expireLine(r, winners)
	ann.next(t, draw.NoticeNoShow)
	for _, tc := range []struct {
		login string
		want  int
	}{{"viewer12", 0}, {"VIEWER11", 1}} {
		applied, err := f.Reroll(t.Context(), draw.SlotAction{Channel: r.Channel, Login: tc.login})
		require.NoError(t, err)
		require.Equal(t, tc.want, applied)
	}
	replacement := ann.next(t, draw.NoticeWinners).Slots[0]
	require.Equal(t, draw.ViewerID("12"), replacement.Viewer)
}

func TestDrawRefusalBatchSurvivesClosing(t *testing.T) {
	for _, phase := range []draw.Phase{draw.PhaseClosing, draw.PhaseCancelled} {
		t.Run(string(phase), func(t *testing.T) {
			ann := make(notices, 5)
			f := newFixture(t, draw.Config{Announcer: ann})
			r := f.open(433, draw.Terms{Kind: draw.KindGiveaway})
			viewers := []draw.RefusedViewer{{Login: "a", Reason: "watch", Need: 7200}, {Login: "b", Reason: "watch", Need: 7200}}
			for i, viewer := range viewers {
				req := draw.Refusal{Round: r, Viewer: draw.ViewerID(strconv.Itoa(i + 1)), Reason: viewer.Reason, Notice: &viewer}
				first, _, err := f.Refuse(t.Context(), req)
				require.NoError(t, err)
				require.True(t, first)
			}
			if phase == draw.PhaseCancelled {
				ok, err := cancelSnapshotGiveaway(f.Store, t.Context(), r)
				require.NoError(t, err)
				require.True(t, ok)
			} else {
				f.close(r.Channel)
			}
			r.Phase = phase
			now := f.now()
			f.shiftFields(r, now, testTimeField("refusal_at", now.Add(-time.Second)))
			f.start(f.Store)
			require.ElementsMatch(t, viewers, ann.next(t, draw.NoticeRefusals).Refused)
			waitRemoved(t, f.client, drawKeys(r))
		})
	}
}
