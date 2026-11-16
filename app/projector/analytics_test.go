// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/channelruntime"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/projection"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStreamAnalyticsCountsOnlyAppliedOnlineEdges(t *testing.T) {
	vc := testValkey(t)
	now := time.Now().UTC()
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker(), analytics.WithClock(func() time.Time { return now }))
	t.Cleanup(rec.Close)
	store := projection.NewStore(vc)
	state := lifecycle.State{Version: 1, RuntimeState: lifecycle.Disabled}
	require.NoError(t, store.SetUser(t.Context(), 1001, projection.UserProjection{AccountCreatedAt: 101, StateRevision: 1, Status: "free", Lifecycle: &state}))
	runtime := channelruntime.NewClient(store, nil, "")
	p := NewProjector(Deps{Store: store, Log: zap.NewNop(), Analytics: rec, Runtime: runtime})
	for _, tc := range []struct {
		kind    string
		version int
		want    int64
	}{
		{kind: "online", version: 1, want: 1},
		{kind: "online", version: 1, want: 1},
		{kind: "online", version: 2, want: 1},
		{kind: "offline", version: 1, want: 1},
		{kind: "offline", version: 3, want: 1},
		{kind: "offline", version: 4, want: 1},
		{kind: "online", version: 5, want: 2},
	} {
		at := now.Add(time.Duration(tc.version) * time.Second)
		event := message(t, strconv.Itoa(tc.version), map[string]any{
			"type":        "stream." + tc.kind,
			"event":       map[string]string{"broadcaster_user_id": "1001"},
			"received_at": at.Format(time.RFC3339Nano),
		})
		require.NoError(t, p.HandleStreamEvent(event))
		rec.Flush(t.Context())
		fields, err := pkgvalkey.HashAllInts(t.Context(), vc, catalog.CounterKey(now))
		require.NoError(t, err)
		require.Equal(t, tc.want, fields["live.starts"])
	}
}
