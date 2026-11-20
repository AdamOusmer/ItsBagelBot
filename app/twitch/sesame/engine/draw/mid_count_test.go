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
		want     []draw.NoticeEvent
	}{
		{"default on", false, []draw.NoticeEvent{draw.NoticeMidCount, draw.NoticeEmpty}},
		{"disabled", true, []draw.NoticeEvent{draw.NoticeEmpty}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ann := make(notices, 5)
			f := newFixture(t, draw.Config{Announcer: ann})
			r := f.openFor(432, draw.Terms{Kind: draw.KindRaffle, Prize: draw.Prize{Points: 100}, MidCountDisabled: tc.disabled}, 2*time.Second)
			f.start(f.Store)
			var events []draw.NoticeEvent
			for {
				n := take(t, ann)
				events = append(events, n.Event)
				if n.Event == draw.NoticeEmpty {
					break
				}
				require.InDelta(t, r.ClosesAt.Sub(r.OpenedAt).Seconds()/2, n.TimeLeft.Seconds(), 0.5)
			}
			require.Equal(t, tc.want, events)
		})
	}
}
