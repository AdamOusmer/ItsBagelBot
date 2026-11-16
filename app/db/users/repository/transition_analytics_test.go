// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"sync/atomic"
	"testing"

	catalog "ItsBagelBot/internal/domain/analytics"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus/bustest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	entcore "entgo.io/ent"
	"github.com/stretchr/testify/require"
)

func (h lifecycleAnalyticsHarness) fields(t *testing.T, key string) map[string]int64 {
	t.Helper()
	h.rec.Flush(t.Context())
	fields, err := pkgvalkey.HashAllInts(t.Context(), h.vc, key)
	require.NoError(t, err)
	return fields
}

func TestEnabledLifecycleCountsOnlyCommittedDesiredChanges(t *testing.T) {
	h := newLifecycleAnalyticsHarness(t)
	for _, tc := range []struct {
		name                      string
		enabled, publicationFails bool
		wantEnabled, wantDisabled int64
	}{
		{name: "already enabled", enabled: true},
		{name: "disable", wantDisabled: 1},
		{name: "already disabled", wantDisabled: 1},
		{name: "enable", enabled: true, wantEnabled: 1, wantDisabled: 1},
		{name: "committed before publication failure", publicationFails: true, wantEnabled: 1, wantDisabled: 2},
		{name: "retry after publication failure", wantEnabled: 1, wantDisabled: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.repo.pub = bustest.NewPublisher()
			if tc.publicationFails {
				h.repo.pub = failingLifecyclePublisher{bustest.NewPublisher()}
			}
			err := h.repo.SetActiveNow(t.Context(), 1001, tc.enabled)
			require.Equal(t, tc.publicationFails, err != nil)
			fields := h.fields(t, catalog.CounterKey(h.now))
			require.Equal(t, []int64{tc.wantEnabled, tc.wantDisabled}, []int64{fields["lifecycle|bot_enabled"], fields["lifecycle|bot_disabled"]})
		})
	}
}

func TestEnabledLifecycleDoesNotCountRecoveryAsOwnerEnable(t *testing.T) {
	h := newLifecycleAnalyticsHarness(t)
	req := h.request(t, "grant_missing")
	req.EventID, req.ObservedAt = "lost", h.now
	_, err := h.repo.Lifecycle(t.Context(), req)
	require.NoError(t, err)
	require.NoError(t, h.repo.SetActiveNow(t.Context(), 1001, true))
	require.Zero(t, h.fields(t, catalog.CounterKey(h.now))["lifecycle|bot_enabled"])
}

func TestLifecycleLostCASDoesNotShiftAccountLevels(t *testing.T) {
	h := newLifecycleAnalyticsHarness(t)
	var intercepted atomic.Bool
	h.repo.client.User.Use(func(next entcore.Mutator) entcore.Mutator {
		return entcore.MutateFunc(func(ctx context.Context, m entcore.Mutation) (entcore.Value, error) {
			if !m.Op().Is(entcore.OpUpdate) {
				return next.Mutate(ctx, m)
			}
			if !intercepted.CompareAndSwap(false, true) {
				return next.Mutate(ctx, m)
			}
			if err := h.repo.client.User.UpdateOneID(1001).SetIsActive(false).Exec(ctx); err != nil {
				return nil, err
			}
			return next.Mutate(ctx, m)
		})
	})
	reply, err := h.repo.Lifecycle(t.Context(), usersrpc.LifecycleRequest{UserID: 1001, Action: "disable"})
	require.NoError(t, err)
	require.True(t, reply.Applied)
	require.Zero(t, h.fields(t, catalog.LevelKey)["accounts.bot_enabled|free"])
}

func TestOnboardingCountsAfterCoalescedPreferenceCommit(t *testing.T) {
	for _, tc := range []struct {
		name             string
		initial, missing bool
		writes           []bool
		want             int64
	}{
		{name: "repeated true", writes: []bool{true, true}, want: 1},
		{name: "coalesced away", writes: []bool{true, false}},
		{name: "coalesced true", writes: []bool{false, true}, want: 1},
		{name: "already onboarded", initial: true, writes: []bool{true}},
		{name: "failed row with committed fallback", missing: true, writes: []bool{true}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newLifecycleAnalyticsHarness(t)
			h.repo.Close(t.Context())
			require.NoError(t, h.repo.client.User.UpdateOneID(1001).SetOnboarded(tc.initial).Exec(t.Context()))
			for _, on := range tc.writes {
				require.NoError(t, h.repo.SetOnboarded(t.Context(), 1001, on))
			}
			if tc.missing {
				require.NoError(t, h.repo.SetOnboarded(t.Context(), 9999, true))
			}
			require.Zero(t, h.fields(t, catalog.CounterKey(h.now))["lifecycle|onboarded"])
			h.repo.Close(t.Context())
			require.Equal(t, tc.want, h.fields(t, catalog.CounterKey(h.now))["lifecycle|onboarded"])
			require.NoError(t, h.repo.SetOnboarded(t.Context(), 1001, tc.writes[len(tc.writes)-1]))
			h.repo.Close(t.Context())
			require.Equal(t, tc.want, h.fields(t, catalog.CounterKey(h.now))["lifecycle|onboarded"])
		})
	}
}
