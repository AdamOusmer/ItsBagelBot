// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"testing"

	catalog "ItsBagelBot/internal/domain/analytics"
	"github.com/stretchr/testify/require"
)

func TestRoadmapCatalogRejectsPrivateDimensions(t *testing.T) {
	for _, metric := range []catalog.Metric{
		catalog.TimersTicks, catalog.TimersTotal, catalog.AutomodFlags, catalog.AccountsRuntime,
		catalog.GamesRounds, catalog.GamesEntrants, catalog.PointsFlows, catalog.RewardsRedeems,
		catalog.AuthLogin, catalog.DesktopGrants, catalog.Lifecycle, catalog.SignupVia,
	} {
		t.Run(string(metric), func(t *testing.T) {
			spec, ok := catalog.Lookup(metric)
			require.True(t, ok)
			dims := make([]string, len(spec.Dims))
			for i, kind := range spec.Dims {
				dims[i] = catalog.Enum(kind)[0]
			}
			for i, kind := range spec.Dims {
				for _, value := range catalog.Enum(kind) {
					row := append([]string(nil), dims...)
					row[i] = value
					require.True(t, catalog.Valid(metric, spec.Type, row, nil), row)
				}
				for _, value := range []string{"123456789", "streamer-login", "private|text", ""} {
					row := append([]string(nil), dims...)
					row[i] = value
					require.False(t, catalog.Valid(metric, spec.Type, row, nil), row)
				}
			}
		})
	}
}
