// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus/bustest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type lifecycleAnalyticsHarness struct {
	repo *Users
	rec  *analytics.Recorder
	vc   valkey.Client
	now  time.Time
	inc  int64
}

func newLifecycleAnalyticsHarness(t *testing.T) lifecycleAnalyticsHarness {
	t.Helper()
	client, repo := lifecycleSetup(t)
	t.Cleanup(func() { repo.Close(t.Context()) })
	require.NoError(t, repo.Register(t.Context(), 1001, "account", "Account", "account@example.test"))
	now := time.Now().UTC()
	vc := valkeytest.Spawn(t)
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker(), analytics.WithClock(func() time.Time { return now }))
	t.Cleanup(rec.Close)
	repo.SetAnalytics(rec)
	return lifecycleAnalyticsHarness{repo: repo, rec: rec, vc: vc, now: now, inc: client.User.GetX(t.Context(), 1001).CreatedAt.UnixMicro()}
}

func (h lifecycleAnalyticsHarness) request(t *testing.T, action string) usersrpc.LifecycleRequest {
	t.Helper()
	current, err := h.repo.Lifecycle(t.Context(), usersrpc.LifecycleRequest{UserID: 1001, Action: "get"})
	require.NoError(t, err)
	return usersrpc.LifecycleRequest{UserID: 1001, AccountCreatedAt: h.inc, AdmissionGeneration: current.State.AdmissionGeneration, Action: action}
}

func (h lifecycleAnalyticsHarness) revoked(t *testing.T) int64 {
	t.Helper()
	h.rec.Flush(t.Context())
	fields, err := pkgvalkey.HashAllInts(t.Context(), h.vc, catalog.CounterKey(h.now))
	require.NoError(t, err)
	return fields["lifecycle|revoked"]
}

func TestLifecycleAnalyticsCountsAcceptedGrantLosses(t *testing.T) {
	h := newLifecycleAnalyticsHarness(t)
	cases := []struct {
		name, action, event string
		offset              time.Duration
		incarnationDelta    int64
		generationDelta     int64
		applied             bool
		revoked             int64
	}{
		{name: "accepted", action: "grant_missing", event: "lost", applied: true, revoked: 1},
		{name: "duplicate", action: "grant_missing", event: "lost", revoked: 1},
		{name: "already missing", action: "grant_missing", event: "same-loss", offset: time.Second, applied: true, revoked: 1},
		{name: "restored", action: "grant_restored", event: "restored", offset: 2 * time.Second, applied: true, revoked: 1},
		{name: "stale timestamp", action: "grant_missing", event: "late", offset: time.Second, revoked: 1},
		{name: "stale generation", action: "grant_missing", event: "old-generation", offset: 3 * time.Second, generationDelta: -1, revoked: 1},
		{name: "stale incarnation", action: "grant_missing", event: "old-account", offset: 3 * time.Second, incarnationDelta: -1, revoked: 1},
		{name: "new loss", action: "grant_missing", event: "lost-again", offset: 4 * time.Second, applied: true, revoked: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := h.request(t, tc.action)
			req.EventID, req.ObservedAt = tc.event, h.now.Add(tc.offset)
			req.AccountCreatedAt += tc.incarnationDelta
			req.AdmissionGeneration += tc.generationDelta
			reply, err := h.repo.Lifecycle(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, tc.applied, reply.Applied)
			require.Equal(t, tc.revoked, h.revoked(t))
		})
	}
}

func TestLifecycleAnalyticsCountsCommittedLossBeforePublication(t *testing.T) {
	h := newLifecycleAnalyticsHarness(t)
	h.repo.pub = failingLifecyclePublisher{bustest.NewPublisher()}
	req := h.request(t, "grant_missing")
	req.EventID, req.ObservedAt = "accepted-loss", h.now
	_, err := h.repo.Lifecycle(t.Context(), req)
	require.Error(t, err)
	require.EqualValues(t, 1, h.revoked(t))

	h.repo.pub = bustest.NewPublisher()
	reply, err := h.repo.Lifecycle(t.Context(), req)
	require.NoError(t, err)
	require.False(t, reply.Applied)
	require.EqualValues(t, 1, h.revoked(t))

	_, err = h.repo.Lifecycle(t.Context(), usersrpc.LifecycleRequest{Action: "due"})
	require.NoError(t, err)
	require.EqualValues(t, 1, h.revoked(t))
}
