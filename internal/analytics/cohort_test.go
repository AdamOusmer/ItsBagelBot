// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"testing"
	"time"

	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestCohortCountsAccountIncarnationsWithinTheSameSignupWeek(t *testing.T) {
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)
	signup := catalog.PeriodStart(catalog.Week, at).Add(time.Hour)
	for _, created := range []time.Time{signup, signup.Add(time.Microsecond), signup} {
		r.Cohort(channel, created)
		r.Distinct(catalog.ChannelsActive, channel)
		r.Flush(t.Context())
	}
	require.Equal(t, int64(2), scard(t, client, catalog.CohortKey(signup, at)))
	for _, window := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		require.Equal(t, int64(1), scard(t, client, catalog.DistinctKey(window, at, string(catalog.ChannelsActive))))
	}
}
