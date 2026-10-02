// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/projector/hydration"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/watchtime"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func TestCommandChangedProjectsValidCommandsAndDropsInvalid(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	p := NewProjector(Deps{Store: store, Log: zap.NewNop()})

	for i, tc := range []struct {
		name         string
		cooldown     uint
		userCooldown uint
		projected    bool
	}{
		{name: "projects a command with both cooldowns at the limit", cooldown: 5, userCooldown: 86400, projected: true},
		{name: "drops a user cooldown over one day", cooldown: 5, userCooldown: 86401},
		{name: "drops a cooldown over one day", cooldown: 86401, userCooldown: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID := uint64(1000 + i)

			err := p.HandleCommandChanged(message(t, "m", data.CommandChangedDTO{
				UserID: userID, Name: "lurk", Response: "hi", Perm: "everyone", IsActive: true,
				Cooldown: tc.cooldown, UserCooldown: tc.userCooldown,
			}))

			require.NoError(t, err, "an invalid event is dropped, never redelivered")
			view, found, _, err := store.GetCommand(t.Context(), userID, "lurk")
			require.NoError(t, err)
			assert.Equal(t, tc.projected, found)
			if tc.projected {
				assert.Equal(t, [2]uint{tc.cooldown, tc.userCooldown}, [2]uint{view.Cooldown, view.UserCooldown})
			}
		})
	}
}

func TestStreamEventsBaselineCountersOnlyOnTheGoLiveEdge(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	nc := testnats.Connect(t)
	hydrator := hydration.New(store, nc, projection.Subjects{
		Users: "test.projection.users", Modules: "test.projection.modules", Commands: "test.projection.commands",
	}, time.Hour, time.Hour, 1, zap.NewNop())
	counters := map[string]int64{
		data.CounterMessagesProcessed: 900, data.CounterEventsProcessed: 1500,
		data.CounterCommandsAnswered: 30, data.CounterModActionsTaken: 4,
	}

	for i, tc := range []struct {
		name       string
		wasLive    bool
		live       bool
		mode       loyaltyMode
		noLoyalty  bool
		seeded     map[projection.CounterName]int64
		want       *projection.StreamCounters
		wantReads  int32
		wantSeeded bool
	}{
		{
			name: "a cold key going live snapshots the loyalty totals", live: true, mode: loyaltyServes,
			want: &projection.StreamCounters{Messages: 900, Answered: 30, ModActions: 4}, wantReads: 4,
		},
		{
			name: "live totals already seeded are used without reading loyalty", live: true, mode: loyaltyServes,
			seeded:    map[projection.CounterName]int64{data.CounterMessagesProcessed: 11, data.CounterCommandsAnswered: 2, data.CounterModActionsTaken: 1},
			want:      &projection.StreamCounters{Messages: 11, Answered: 2, ModActions: 1},
			wantReads: 0,
		},
		{name: "an already live re-delivery writes no baseline", wasLive: true, live: true, mode: loyaltyServes},
		{name: "going offline writes no baseline", wasLive: true, mode: loyaltyServes},
		{name: "an already offline re-delivery writes no baseline", mode: loyaltyServes},
		{name: "a refusing loyalty service skips the baseline", live: true, mode: loyaltyRefuses, wantReads: 1},
		{name: "an unreachable loyalty service skips the baseline", live: true, mode: loyaltyAbsent},
		{name: "a missing loyalty client skips the baseline", live: true, noLoyalty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			broadcasterID := uint64(5000 + i)
			live := newFakeLiveStore()
			if tc.seeded != nil {
				live.totals[broadcasterID] = tc.seeded
			}
			deps := Deps{Store: store, Hydrator: hydrator, Live: live, Log: zap.NewNop()}
			service := &loyaltyService{}
			if !tc.noLoyalty {
				var loyalty *loyaltyCounters
				service, loyalty = newLoyalty(t, tc.mode, counters, nil)
				deps.Loyalty = loyalty
			}
			require.NoError(t, store.SetStreamLive(ctx, broadcasterID, tc.wasLive))
			eventType := map[bool]string{true: "stream.online", false: "stream.offline"}[tc.live]

			err := NewProjector(deps).HandleStreamEvent(message(t, "evt", map[string]any{
				"subscription": map[string]string{"type": eventType},
				"event":        map[string]string{"broadcaster_user_id": strconv.FormatUint(broadcasterID, 10)},
			}))

			require.NoError(t, err)
			isLive, _, err := store.GetStreamLive(ctx, broadcasterID)
			require.NoError(t, err)
			assert.Equal(t, tc.live, isLive)
			baseline, found, err := store.GetStreamCounterBaseline(ctx, strconv.FormatUint(broadcasterID, 10))
			require.NoError(t, err)
			var got *projection.StreamCounters
			if found {
				got = &baseline
			}
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantReads, service.reads.Load())
		})
	}
}

func TestWatchIgnoredDeletionPreservesLiveCounters(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	defer client.Close()
	ctx := t.Context()
	id := uint64(time.Now().UnixNano()%100000000 + 8600000000)
	sid := strconv.FormatUint(id, 10)
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key("settings:"+sid, watchtime.AdmissionKey(id)).Build())
	})
	store := projection.NewStore(client)
	require.NoError(t, store.SetUser(ctx, id, projection.UserProjection{AccountCreatedAt: 101, StateRevision: 1, IsActive: true, Status: "paid"}))
	live := newFakeLiveStore()
	projector := NewProjector(Deps{Store: store, Live: live, Log: zap.NewNop()})
	for _, epoch := range []int64{0, 100} {
		require.NoError(t, projector.HandleUserDeleted(message(t, "deleted", data.UserDeletedDTO{UserID: id, AccountCreatedAt: epoch})))
		require.Zero(t, live.deleted, "ignored deletion must not touch the recreated account's live totals")
		_, active, _, _, _, err := store.GetUser(ctx, id)
		require.NoError(t, err)
		require.True(t, active)
	}
}
