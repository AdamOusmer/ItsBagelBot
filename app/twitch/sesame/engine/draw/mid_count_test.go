// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"

	"github.com/stretchr/testify/require"
)

func TestRaffleMidCount(t *testing.T) {
	for _, tc := range []struct {
		name     string
		disabled bool
		entered  bool
		want     []draw.NoticeEvent
	}{
		{"default on", false, true, []draw.NoticeEvent{draw.NoticeMidCount, draw.NoticeDrawn, draw.NoticeWinners}},
		{"disabled", true, true, []draw.NoticeEvent{draw.NoticeDrawn, draw.NoticeWinners}},
		{"no entries", false, false, []draw.NoticeEvent{draw.NoticeEmpty}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ann := make(notices, 5)
			f := newFixture(t, draw.Config{Announcer: ann})
			r := f.openFor(432, draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 100}, MidCountDisabled: tc.disabled}, 2*time.Second)
			if tc.entered {
				f.join(r, "7")
			}
			f.start(f.Store)
			var events []draw.NoticeEvent
			for {
				n := take(t, ann)
				events = append(events, n.Event)
				if n.Event == draw.NoticeEmpty || n.Event == draw.NoticeWinners {
					break
				}
				checkMidCount(t, r, n)
			}
			require.Equal(t, tc.want, events)
		})
	}
}

func checkMidCount(t *testing.T, r draw.Round, n draw.Notice) {
	t.Helper()
	if n.Event == draw.NoticeMidCount {
		require.InDelta(t, r.ClosesAt.Sub(r.OpenedAt).Seconds()/2, n.TimeLeft.Seconds(), 0.5)
		require.Equal(t, 1, n.Entries)
	}
}
