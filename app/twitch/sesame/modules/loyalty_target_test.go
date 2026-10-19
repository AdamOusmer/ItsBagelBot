// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPointsGiveSenderComesFromTheEnvelopeID(t *testing.T) {
	fake := &fakeLoyalty{}
	c := loyaltyChat(viewer, "")
	c.Env.ChatterUserLogin = "renamedviewer"

	out := runLoyaltyChat(t, fake, c, "!points give @Blemmyz 5")

	require.Len(t, out, 1)
	assert.Equal(t, []transferCall{{fromID: 7, targetID: 8, login: "blemmyz", amount: 5}}, fake.transfers)
}

func TestPointsAdjustTargetsTheMentionedIDNotTheStoredLogin(t *testing.T) {
	fake := &fakeLoyalty{}
	c := loyaltyChat(streamer, "")
	c.Env.Mentions = []lane.Mention{{UserID: "99", UserLogin: "newname", UserName: "NewName"}}

	runLoyaltyChat(t, fake, c, "!points add @NewName 10")

	assert.Equal(t, []adjustCall{{viewerID: 99, login: "newname", value: 10}}, fake.adjusts)
}

func TestPointsTargetNeedsAnAtMention(t *testing.T) {
	for _, tc := range []struct {
		text     string
		contains string
	}{
		{"!points give bob 100", "mention them with @"},
		{"!points add bob 10", "mention them with @"},
		{"!points give @ghost 100", "was not found"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			fake := &fakeLoyalty{}
			c := loyaltyChat(streamer, "")
			c.Env.Mentions = []lane.Mention{}

			out := runLoyaltyChat(t, fake, c, tc.text)

			require.Len(t, out, 1)
			assert.Contains(t, out[0].Text, tc.contains)
			assert.Empty(t, fake.transfers)
			assert.Empty(t, fake.adjusts)
		})
	}
}

func TestDuelCallsCarryTheChatterID(t *testing.T) {
	fake := &fakeDuel{joinRes: engine.DuelJoinResult{Open: true, Joined: true}}

	runChat(t, Duel(duelDeps(fake)), chatCtx("42", "renamedbob"), "!duel 100")

	assert.Equal(t, []uint64{42}, fake.callerIDs)
	assert.Equal(t, "renamedbob", fake.joinLogin)
}

func TestDuelChallengeTargetComesFromTheMention(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mentions []lane.Mention
		text     string
		contains string
		opened   bool
	}{
		{"mentioned target", []lane.Mention{{UserID: "77", UserLogin: "crust"}}, "!duel @Crust 400", "challenges", true},
		{"no mention fragment", []lane.Mention{}, "!duel @Crust 400", "haven't seen", false},
		{"plain word without an at sign", []lane.Mention{}, "!duel crust 400", "mention your opponent with @", false},
		{"self under another login", []lane.Mention{{UserID: "42", UserLogin: "crust"}}, "!duel @Crust 400", "can't duel yourself", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeDuel{openRes: engine.DuelOpenResult{Started: true}}
			c := chatCtx("42", "maya")
			c.Env.Mentions = tc.mentions

			out := runChat(t, Duel(duelDeps(fake)), c, tc.text)

			require.Len(t, out, 1)
			assert.Contains(t, strings.ToLower(out[0].Text), tc.contains)
			if tc.opened {
				assert.Equal(t, engine.DuelParty{ID: 77, Login: "crust"}, fake.openSpec.Challenged)
			} else {
				assert.Zero(t, fake.openSpec)
			}
		})
	}
}
