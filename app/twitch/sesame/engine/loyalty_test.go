// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTierMultiplier(t *testing.T) {
	cases := []struct {
		tier string
		want int64
	}{
		{"1000", 1},
		{"2000", 2},
		{"3000", 6},
		{"", 1},
		{"prime", 1},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, TierMultiplier(tc.tier), "tier %q", tc.tier)
	}
}

type effectiveLoyalty struct {
	name                                        string
	sub, resub, gift, cheerPer100, watchPerTick int64
}

func effectiveValues(cfg LoyaltyModuleConfig) effectiveLoyalty {
	return effectiveLoyalty{
		name: cfg.Name(), sub: cfg.EffectiveSubPoints(), resub: cfg.EffectiveResubPoints(),
		gift: cfg.EffectiveGiftSubPoints(), cheerPer100: cfg.EffectiveCheerPointsPer100(),
		watchPerTick: cfg.EffectiveWatchPointsPerTick(),
	}
}

func TestLoyaltyConfigEffectiveValues(t *testing.T) {
	cases := []struct {
		name string
		cfg  LoyaltyModuleConfig
		want effectiveLoyalty
	}{
		{
			name: "an empty config takes the defaults",
			want: effectiveLoyalty{
				name: "points", sub: defaultSubPoints, resub: defaultResubPoints, gift: defaultGiftSubPoints,
				cheerPer100: defaultCheerPointsPer100, watchPerTick: defaultWatchPointsPerTick,
			},
		},
		{
			name: "configured values win and a negative rate floors at zero",
			cfg:  LoyaltyModuleConfig{PointsName: "bagels", SubPoints: 100, CheerPointsPer100: -1},
			want: effectiveLoyalty{
				name: "bagels", sub: 100, resub: defaultResubPoints, gift: defaultGiftSubPoints,
				cheerPer100: 0, watchPerTick: defaultWatchPointsPerTick,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, effectiveValues(tc.cfg))
		})
	}
}

func TestCounterBumpRefusesSystemCounters(t *testing.T) {
	s := &ValkeyLoyaltyStore{}
	for _, name := range data.SystemCounterNames() {
		_, err := s.CounterBump(context.Background(), CounterBump{BroadcasterID: 1, Name: name, Delta: 1})
		assert.ErrorIs(t, err, ErrReservedCounter, name)
	}
	_, err := s.CounterBump(context.Background(), CounterBump{BroadcasterID: 1, Name: " !Commands_Answered ", Delta: 1})
	assert.ErrorIs(t, err, ErrReservedCounter, "normalization must not open a way around the guard")
}

func TestLoyaltyConfigRejectsMalformedRates(t *testing.T) {
	for _, raw := range []string{`{"watchPointsPerTick":"off"}`, `{"subPoints":`, `[]`} {
		cfg, enabled := ReadLoyaltyConfig(context.Background(), fakeReader{modules: map[string]projection.ModuleView{
			LoyaltyModuleName: {IsEnabled: true, Configs: []byte(raw)},
		}}, 7)
		assert.False(t, enabled, raw)
		assert.Equal(t, LoyaltyModuleConfig{}, cfg)
	}
}

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
