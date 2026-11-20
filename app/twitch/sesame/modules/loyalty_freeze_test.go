// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/event/lane"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type freezeStore struct {
	sets, releases []engine.FreezeCommand
	changed        bool
	err            error
}

func (s *freezeStore) FreezeSet(_ context.Context, command engine.FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	s.sets = append(s.sets, command)
	end := time.Now().Add(time.Duration(command.Minutes) * time.Minute)
	view := loyaltyrpc.FreezeView{FreezeStamp: loyaltyrpc.FreezeStamp{ExpiresAt: end.UnixMilli()}}
	return view, s.changed, s.err
}

func (s *freezeStore) FreezeRelease(_ context.Context, command engine.FreezeCommand) (loyaltyrpc.FreezeView, bool, error) {
	s.releases = append(s.releases, command)
	return loyaltyrpc.FreezeView{}, s.changed, s.err
}

type freezeChatCase struct {
	name, command, reply    string
	badge                   string
	minutes, sets, releases int
	changed                 bool
	err                     error
}

func TestLoyaltyFreezeCommandsProcess(t *testing.T) {
	for _, tc := range []freezeChatCase{
		{name: "bare freeze", command: "!freeze", badge: "moderator", minutes: 30, sets: 1, changed: true, reply: "Leaderboard paused for 30 min. Points and watch time are still counting."},
		{name: "active freeze", command: "!freeze 6", badge: "moderator", minutes: 6, sets: 1, reply: "@viewer the leaderboard is already paused, 6 min left."},
		{name: "invalid minutes", command: "!freeze 31", badge: "moderator", reply: "Usage: !freeze [1-30]"},
		{name: "viewer cannot freeze", command: "!freeze"},
		{name: "released", command: "!unfreeze", badge: "moderator", releases: 1, changed: true, reply: "Leaderboard unpaused. Standings are live again."},
		{name: "already live", command: "!unfreeze", badge: "moderator", releases: 1, reply: "@viewer the leaderboard is not paused."},
		{name: "round still holds", command: "!unfreeze", badge: "moderator", releases: 1,
			err:   bus.RPCReplyError{Code: domainrpc.CodeConflict, Message: loyaltyrpc.RefusalHeldByRound},
			reply: "@viewer A giveaway is using the frozen standings; drawing or cancelling it unfreezes."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &freezeStore{changed: tc.changed, err: tc.err}
			pub := processFreezeChat(t, store, tc)
			require.Len(t, store.sets, tc.sets)
			require.Len(t, store.releases, tc.releases)
			want := engine.FreezeCommand{BroadcasterID: 2, Holder: loyaltyrpc.FreezeModHolder, Minutes: tc.minutes}
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
