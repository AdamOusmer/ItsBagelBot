// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestReaderUnionsMixedDistinctFormatsInEachWindow(t *testing.T) {
	for _, window := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		t.Run(catalog.WindowCode(window), func(t *testing.T) {
			client := valkeytest.Spawn(t)
			ctx := t.Context()
			at := catalog.PeriodStart(window, day)
			set := catalog.DistinctKey(window, at, string(catalog.ChannelsActive))
			sketch := strings.Replace(set, "an:s", "an:h", 1)
			require.NoError(t, client.Do(ctx, client.B().Pfadd().Key(sketch).Element("a", "b").Build()).Error())
			require.NoError(t, client.Do(ctx, client.B().Sadd().Key(set).Member("b", "c").Build()).Error())
			require.NoError(t, client.Do(ctx, client.B().Sadd().Key(catalog.IndexKey(window, at)).Member(set, sketch).Build()).Error())

			rows, err := analytics.NewReader(client, nil).Read(ctx, analytics.Query{Metrics: []catalog.Metric{catalog.ChannelsActive}, Window: window, From: at, Now: at})
			require.NoError(t, err)
			require.Equal(t, []analytics.Row{{Window: window, Start: at, Metric: catalog.ChannelsActive, Dims: []string{}, Value: 3}}, rows)
		})
	}
}
