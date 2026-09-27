// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoyaltyEventStreamerPointsPreference(t *testing.T) {
	for _, event := range []struct {
		name   string
		extra  string
		points int64
	}{
		{"channel.subscribe", `,"tier":"1000"`, 500},
		{"channel.subscription.message", `,"tier":"2000"`, 1000},
		{"channel.subscription.gift", `,"total":3`, 300},
		{"channel.cheer", `,"bits":200`, 100},
	} {
		for _, tc := range []struct {
			name   string
			viewer string
			config string
			earns  bool
		}{
			{"streamer default", "2", "", true},
			{"streamer off", "2", `{"streamerPoints":-1}`, false},
			{"viewer while streamer off", "7", `{"streamerPoints":-1}`, true},
		} {
			t.Run(event.name+"/"+tc.name, func(t *testing.T) {
				fake := &fakeLoyalty{}
				m := loyaltyModule(t, fake)
				payload := fmt.Sprintf(`{"user_id":"%s","user_login":"person"%s}`, tc.viewer, event.extra)
				var col collector
				require.NoError(t, m.Events[event.name](context.Background(), loyaltyCtx(event.name, payload, tc.config), col.emit))
				if !tc.earns {
					require.Empty(t, fake.earns)
					return
				}
				require.Len(t, fake.earns, 1)
				require.Equal(t, event.points, fake.earns[0].points)
			})
		}
	}
}
