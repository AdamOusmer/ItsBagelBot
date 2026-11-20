// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type giveawayLifecycleProjection struct {
	projection.Reader
}

func (giveawayLifecycleProjection) Module(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	raw := codec.RawMessage(`{"defaultPrize":"Game","windowSeconds":600,"defaultPreset":"all","presets":[{"name":"all","freezeWhileOpen":true}]}`)
	return projection.ModuleView{Name: engine.GiveawayModuleName, IsEnabled: true, Configs: raw}, true, nil
}

type giveawayLifecycleFreeze struct {
	engine.LoyaltyFreeze
	set       engine.FreezeCommand
	release   chan engine.FreezeCommand
	expiresAt int64
}

func (s *giveawayLifecycleFreeze) FreezeSet(_ context.Context, req engine.FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	s.set = req
	return loyaltyrpc.FreezeView{FreezeStamp: loyaltyrpc.FreezeStamp{FreezeID: "snapshot", ExpiresAt: s.expiresAt}}, true, nil
}

func (s *giveawayLifecycleFreeze) FreezeRelease(_ context.Context, req engine.FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	s.release <- req
	return loyaltyrpc.FreezeView{}, true, nil
}

type giveawayLifecycleCase struct {
	name   string
	action func(*draw.Store, context.Context, draw.Round) (bool, error)
	window time.Duration
}

func TestGiveawayFreezeLifecycle(t *testing.T) {
	for _, tc := range []giveawayLifecycleCase{
		{name: "manual close", action: closeHeldGiveaway, window: 2 * time.Minute},
		{name: "cancel", action: cancelHeldGiveaway, window: 2 * time.Minute},
		{name: "timer", window: time.Second},
	} {
		t.Run(tc.name, tc.run)
	}
}

func (tc giveawayLifecycleCase) run(t *testing.T) {
	freeze := &giveawayLifecycleFreeze{release: make(chan engine.FreezeCommand, 2)}
	f := newFixture(t, draw.Config{Phases: engine.GiveawayPhaseObserver{Freeze: freeze, Log: zap.NewNop()}})
	freeze.expiresAt = f.now().Add(tc.window).UnixMilli()
	deps := engine.Deps{Draw: f.Store, LoyaltyFreeze: freeze, Proj: giveawayLifecycleProjection{}}
	outcome, err := engine.StartGiveaway(t.Context(), deps, engine.GiveawayStart{Channel: 42, Actor: "7"})
	require.NoError(t, err)
	require.Empty(t, outcome.Refusal)
	tc.checkRound(f, outcome.Round, freeze)
}

func (tc giveawayLifecycleCase) checkRound(f fixture, r draw.Round, freeze *giveawayLifecycleFreeze) {
	f.t.Helper()
	ctx := f.t.Context()
	holder := loyaltyrpc.FreezeRoundPrefix + string(r.ID)
	require.Equal(f.t, engine.FreezeCommand{BroadcasterID: 42, Holder: holder, Minutes: int(loyaltyrpc.FreezeMaximum / time.Minute)}, freeze.set)
	require.Equal(f.t, freeze.expiresAt, r.ClosesAt.UnixMilli())
	f.t.Cleanup(func() {
		require.NoError(f.t, drawKeys(r).Teardown(context.Background(), f.client, string(r.ID)))
	})
	f.start(f.Store)
	if tc.action != nil {
		tc.applyAction(f, ctx, r)
	}
	require.Equal(f.t, engine.FreezeCommand{BroadcasterID: 42, Holder: holder, FreezeID: "snapshot"}, take(f.t, freeze.release))
	require.Empty(f.t, freeze.release)
}

func closeHeldGiveaway(s *draw.Store, ctx context.Context, r draw.Round) (bool, error) {
	return s.Close(ctx, draw.CloseRequest{Channel: r.Channel, Round: r.ID, Kind: draw.KindGiveaway})
}

func cancelHeldGiveaway(s *draw.Store, ctx context.Context, r draw.Round) (bool, error) {
	return s.Cancel(ctx, draw.ModAction{Channel: r.Channel, Round: r.ID, Kind: draw.KindGiveaway})
}

func (tc giveawayLifecycleCase) applyAction(f fixture, ctx context.Context, r draw.Round) {
	f.t.Helper()
	extended, extendErr := f.Extend(ctx, draw.ExtendRequest{Channel: r.Channel, Round: r.ID, Kind: draw.KindGiveaway})
	applied, actionErr := tc.action(f.Store, ctx, r)
	require.NoError(f.t, extendErr)
	require.NoError(f.t, actionErr)
	require.Equal(f.t, []bool{false, true}, []bool{extended, applied})
}

func TestGiveawayRoundTargetRejectsStaleDashboardActions(t *testing.T) {
	f := newFixture(t, draw.Config{})
	prior := f.open(42, draw.Terms{Kind: draw.KindGiveaway})
	closed, err := closeHeldGiveaway(f.Store, t.Context(), prior)
	require.NoError(t, err)
	require.True(t, closed)
	current := f.open(prior.Channel, prior.Terms)
	actions := []func() (bool, error){
		func() (bool, error) { return closeHeldGiveaway(f.Store, t.Context(), prior) },
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
