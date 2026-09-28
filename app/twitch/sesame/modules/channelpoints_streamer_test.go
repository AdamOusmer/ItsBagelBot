// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestChannelPointsStreamerPointsPreference(t *testing.T) {
	cfg := `{"rewards":[{"id":"r1","action":"chat","message":"{user} bought points","points":250}]}`
	for _, tc := range []struct {
		name    string
		redeem  string
		loyalty string
		earns   bool
	}{
		{"streamer default", "2", `{}`, true},
		{"streamer off", "2", `{"streamerPoints":-1}`, false},
		{"viewer while streamer off", "7", `{"streamerPoints":-1}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLoyalty{}
			proj := &fakeProj{modules: []projection.ModuleView{{Name: engine.LoyaltyModuleName, IsEnabled: true, Configs: []byte(tc.loyalty)}}}
			m := ChannelPoints(engine.Deps{Loyalty: fake, Proj: proj, Log: zap.NewNop()})
			ev := `{"id":"red1","broadcaster_user_id":"2","user_id":"` + tc.redeem + `","user_name":"Person","user_login":"person","reward":{"id":"r1","title":"Point Pack","cost":100}}`
			var col collector
			require.NoError(t, m.Events[redemptionAddType](context.Background(), loyaltyCtx(redemptionAddType, ev, cfg), col.emit))
			if !tc.earns {
				assert.Empty(t, fake.earns)
				return
			}
			require.Len(t, fake.earns, 1)
			assert.EqualValues(t, 250, fake.earns[0].points)
		})
	}
}
