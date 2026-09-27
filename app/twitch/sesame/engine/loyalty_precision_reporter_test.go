// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"math"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLoyaltyReporterCapsEarnedPointsBeforePublish(t *testing.T) {
	pub := &rawPublisher{}
	reporter := NewLoyaltyReporter(pub, zap.NewNop())
	reporter.Earn(1, 7, "viewer", "Viewer", math.MaxInt64-1, 300)
	reporter.Earn(1, 7, "", "", 10, 300)
	reporter.Earn(1, 7, "", "", 1, 0)
	reporter.Earn(1, 8, "other", "Other", 20, 300)
	reporter.Close()

	payloads := pub.payloads[data.SubjectLoyaltyEarned]
	require.Len(t, payloads, 1)
	var award data.LoyaltyEarnedDTO
	require.NoError(t, codec.Unmarshal(payloads[0], &award))
	require.EqualValues(t, 1, award.UserID)
	entries := map[uint64]data.LoyaltyEarnEntry{}
	for _, entry := range award.Entries {
		entries[entry.ViewerID] = entry
	}
	require.Len(t, entries, 2)
	require.EqualValues(t, math.MaxInt64, entries[7].Points, "published aggregate must remain within BIGINT")
	require.EqualValues(t, 600, entries[7].WatchSeconds, "point cap must preserve watch time")
	require.Equal(t, "viewer", entries[7].ViewerLogin)
	require.Equal(t, "Viewer", entries[7].ViewerName)
	require.EqualValues(t, 20, entries[8].Points, "point cap must preserve other viewers")
	require.EqualValues(t, 300, entries[8].WatchSeconds)
}
