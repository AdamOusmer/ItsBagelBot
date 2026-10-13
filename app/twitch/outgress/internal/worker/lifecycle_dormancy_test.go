package worker

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/projection"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestDormancyUsesOrderedLiveFactsAndOneProbeWindow(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name        string
		state       lifecycle.State
		live        projection.StreamLive
		unavailable bool
		want        string
	}{
		{name: "fresh positive postpones", live: projection.StreamLive{Live: true, Known: true, Version: now.UnixMilli()}, want: "live"},
		{name: "newest offline permits dormancy", live: projection.StreamLive{Known: true, Version: now.UnixMilli()}, want: "offline"},
		{name: "stale positive gets bounded probe", live: projection.StreamLive{Live: true, Known: true, Version: now.Add(-time.Hour).UnixMilli()}, want: "probe"},
		{name: "same window never probes again", state: lifecycle.State{DormancyProbeAt: now.Add(-time.Hour)}, live: projection.StreamLive{Live: true, Known: true, Version: now.Add(-time.Hour).UnixMilli()}, want: "defer"},
		{name: "cold cache defers without polling", want: "defer"},
		{name: "outage defers", unavailable: true, want: "defer"},
		{name: "outage reaches24hour limit", state: lifecycle.State{DormancyUnknownSince: now.Add(-24 * time.Hour)}, unavailable: true, want: "unavailable"},
		{name: "old positive cannot defer forever", state: lifecycle.State{DormancyUnknownSince: now.Add(-24 * time.Hour), DormancyProbeAt: now.Add(-time.Hour)}, live: projection.StreamLive{Known: true, Live: true, Version: now.Add(-24 * time.Hour).UnixMilli()}, want: "unavailable"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, dormancyDecision(tt.state, tt.live, tt.unavailable, now))
		})
	}
}
