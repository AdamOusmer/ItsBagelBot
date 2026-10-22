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

func TestPointsBareLoginResolvesThroughTwitch(t *testing.T) {
	for _, tc := range []struct {
		text      string
		transfers []transferCall
		adjusts   []adjustCall
	}{
		{"!points give Bob 100", []transferCall{{fromID: 2, targetID: 8, login: "bob", amount: 100}}, nil},
		{"!points set bob 0", nil, []adjustCall{{viewerID: 8, login: "bob", value: 0, absolute: true}}},
		{"!setpoints bob 250", nil, []adjustCall{{viewerID: 8, login: "bob", value: 250, absolute: true}}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			fake := &fakeLoyalty{}
			c := loyaltyChat(streamer, "")
			c.Env.Mentions = []lane.Mention{}

			runLoyaltyChat(t, fake, c, tc.text)

			assert.Equal(t, tc.transfers, fake.transfers)
			assert.Equal(t, tc.adjusts, fake.adjusts)
		})
	}
}

func TestPointsUnknownTargetIsNotFound(t *testing.T) {
	for _, text := range []string{"!points give ghost 100", "!points add ghost 10", "!points give @ghost 100"} {
		t.Run(text, func(t *testing.T) {
			fake := &fakeLoyalty{}
			d := loyaltyDeps(fake)
			d.TwitchAccounts = &fakeAccountAge{}
			c := loyaltyChat(streamer, "")
			c.Env.Mentions = []lane.Mention{}

			out := runChat(t, Loyalty(d), c, text)

			require.Len(t, out, 1)
			assert.Contains(t, out[0].Text, "was not found")
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

func TestDuelChallengeTargetResolvesToAnID(t *testing.T) {
	crust := engine.AccountAgeResult{TargetID: "77", UserFound: true}
	for _, tc := range []struct {
		name     string
		mentions []lane.Mention
		lookup   engine.AccountAgeResult
		text     string
		contains string
		opened   bool
	}{
		{"mentioned target", []lane.Mention{{UserID: "77", UserLogin: "crust"}}, engine.AccountAgeResult{}, "!duel @Crust 400", "challenges", true},
		{"no mention fragment", []lane.Mention{}, crust, "!duel @Crust 400", "was not found", false},
		{"plain word resolves through twitch", []lane.Mention{}, crust, "!duel crust 400", "challenges", true},
		{"plain word for an unknown user", []lane.Mention{}, engine.AccountAgeResult{}, "!duel crust 400", "was not found", false},
		{"self under another login", []lane.Mention{{UserID: "42", UserLogin: "crust"}}, engine.AccountAgeResult{}, "!duel @Crust 400", "can't duel yourself", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeDuel{openRes: engine.DuelOpenResult{Started: true}}
			c := chatCtx("42", "maya")
			c.Env.Mentions = tc.mentions
			d := duelDeps(fake)
			d.TwitchAccounts = &fakeAccountAge{result: tc.lookup}

			out := runChat(t, Duel(d), c, tc.text)

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
