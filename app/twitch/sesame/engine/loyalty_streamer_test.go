// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/domain/rpc/manage"

	"github.com/stretchr/testify/require"
)

func TestWatchAwardsStreamerPointsPreference(t *testing.T) {
	reply := manage.ChattersReply{Chatters: []manage.Chatter{
		{ID: "42", Login: "streamer"},
		{ID: "7", Login: "viewer"},
		{ID: "42", Login: "streamer"},
		{ID: "99", Login: "bot"},
	}}
	page := loyaltyPage{id: 42, clock: &ValkeyLoyaltyClock{botID: "99"}}
	for _, tc := range []struct {
		name           string
		raw            string
		streamerPoints int64
	}{
		{"existing config stays on", `{ "watchPointsPerTick": 15 }`, 15},
		{"explicitly on", `{ "watchPointsPerTick": 15, "streamerPoints": 0 }`, 15},
		{"off", `{ "watchPointsPerTick": 15, "streamerPoints": -1 }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := decodeWatchConfiguration([]byte(tc.raw))
			require.NoError(t, err)
			entries := page.viewerAwards(reply, cfg)
			require.Len(t, entries, 2)
			require.EqualValues(t, 42, entries[0].ViewerID)
			require.Equal(t, tc.streamerPoints, entries[0].Points)
			require.EqualValues(t, 300, entries[0].WatchSeconds, "disabling points must retain streamer watch time")
			require.EqualValues(t, 7, entries[1].ViewerID)
			require.EqualValues(t, 15, entries[1].Points, "viewer earnings must stay unchanged")
			require.EqualValues(t, 300, entries[1].WatchSeconds)
		})
	}
}
