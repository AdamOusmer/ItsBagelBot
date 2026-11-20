// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/event/lane"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type freezeStore struct {
	sets, releases []freezeCall
	changed        bool
	active         bool
	gets           int
}

type freezeCall struct {
	channel draw.ChannelID
	minutes engine.FreezeMinutes
}

func (s *freezeStore) FreezeGet(context.Context, draw.ChannelID) (loyaltyrpc.FreezeView, error) {
	s.gets++
	return loyaltyrpc.FreezeView{Active: s.active}, nil
}

func (s *freezeStore) FreezeSet(_ context.Context, channel draw.ChannelID, minutes engine.FreezeMinutes) (loyaltyrpc.FreezeTimes, error) {
	s.sets = append(s.sets, freezeCall{channel: channel, minutes: minutes})
	return loyaltyrpc.FreezeTimes{}, nil
}

func (s *freezeStore) FreezeRelease(_ context.Context, channel draw.ChannelID) (bool, error) {
	s.releases = append(s.releases, freezeCall{channel: channel})
	return s.changed, nil
}

type freezeChatCase struct {
	name, command, reply string
	badge                string
	minutes              engine.FreezeMinutes
	sets, releases       int
	changed              bool
}

func TestLoyaltyFreezeCommandsProcess(t *testing.T) {
	for _, tc := range []freezeChatCase{
		{name: "bare freeze", command: "!freeze", badge: "moderator", minutes: 30, sets: 1, changed: true, reply: "Leaderboard paused for 30 min. Points and watch time are still counting."},
		{name: "restart freeze", command: "!freeze 20", badge: "moderator", minutes: 20, sets: 1, reply: "Leaderboard paused for 20 min. Points and watch time are still counting."},
		{name: "invalid minutes", command: "!freeze 31", badge: "moderator", reply: "Usage: !freeze [1-30]"},
		{name: "viewer cannot freeze", command: "!freeze"},
		{name: "released", command: "!unfreeze", badge: "moderator", releases: 1, changed: true, reply: "Leaderboard unpaused. Standings are live again."},
		{name: "already live", command: "!unfreeze", badge: "moderator", releases: 1, reply: "@viewer the leaderboard is not paused."},
		{name: "viewer cannot unfreeze", command: "!unfreeze"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &freezeStore{changed: tc.changed}
			pub := processFreezeChat(t, store, tc)
			require.Len(t, store.sets, tc.sets)
			require.Len(t, store.releases, tc.releases)
			want := freezeCall{channel: 2, minutes: tc.minutes}
			for _, command := range append(store.sets, store.releases...) {
				assert.Equal(t, want, command)
			}
			assertFreezeChatReply(t, pub, tc)
		})
	}
}

func processFreezeChat(t *testing.T, store *freezeStore, tc freezeChatCase) *variablePublisher {
	t.Helper()
	pub := &variablePublisher{}
	deps := engine.Deps{
		Proj: &fakeProj{modules: loyaltyView(true, "{}")}, LoyaltyFreeze: store, Pub: pub,
		Log: zap.NewNop(), Cooldown: engine.NoopCooldown{},
		Dedup: engine.NewEventDedup(loyaltyClaims{}, "", time.Hour, nil),
	}
	pipe := engine.NewPipeline(deps, engine.NewRegistry(deps.Log, Loyalty(deps)), engine.Config{OutgressStandard: "outgress.standard"})
	t.Cleanup(pipe.Close)
	env := lane.Envelope{Type: "channel.chat.message", Lane: "standard", MsgID: "freeze-test",
		BroadcasterUserID: "2", BroadcasterUserLogin: "streamer", ChatterUserID: "7", ChatterUserLogin: "viewer", Text: tc.command}
	if tc.badge != "" {
		env.Badges = []lane.Badge{{SetID: tc.badge}}
	}
	body, err := codec.Marshal(env)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, pipe.Process(bus.NewMessage(env.MsgID, body)))
	}
	return pub
}

func assertFreezeChatReply(t *testing.T, pub *variablePublisher, tc freezeChatCase) {
	t.Helper()
	if tc.reply == "" {
		assert.Empty(t, pub.messages)
		return
	}
	require.Len(t, pub.messages, 1)
	var payload struct {
		Message string `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(pub.messages[0].Payload, &payload))
	assert.Equal(t, tc.reply, payload.Message)
}
