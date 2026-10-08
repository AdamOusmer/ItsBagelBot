// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"errors"
	"fmt"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type liveState int

const (
	liveUnwired liveState = iota
	liveOffline
	liveOnline
	liveBroken
)

func (s liveState) store() engine.LiveStore {
	switch s {
	case liveOffline:
		return &fakeLive{live: false}
	case liveOnline:
		return &fakeLive{live: true}
	case liveBroken:
		return &fakeLive{err: errors.New("live store unavailable")}
	}
	return nil
}

func redemptionPayload(userID, input string) string {
	return fmt.Sprintf(`{"id":"redeem-1","broadcaster_user_id":"2","broadcaster_user_login":"streamer","user_id":%q,"user_name":"CoolViewer","user_login":"coolviewer","user_input":%q,"reward":{"id":"rw-1","title":"Say hi","cost":500}}`, userID, input)
}

func rewardChat(text string) module.Output {
	return module.Output{Type: outgress.TypeChat, BroadcasterID: "2", Text: text}
}

func rewardUpdate(status string) module.Output {
	return module.Output{Type: outgress.TypeRedemptionUpdate, BroadcasterID: "2", RewardID: "rw-1", RedemptionID: "redeem-1", Status: status}
}

func rewardConfig(binding string) string {
	return `{"rewards":[` + binding + `]}`
}

type rewardCase struct {
	name    string
	config  string
	userID  string
	input   string
	live    liveState
	loyalty string
	want    []module.Output
	earns   []earnCall
	bumps   []bumpCall
}

var rewardCases = []rewardCase{
	{name: "no bindings does nothing", config: `{"rewards":[]}`},
	{name: "an unmatched reward does nothing", config: rewardConfig(`{"id":"other","action":"chat","message":"hi"}`)},
	{name: "chat action expands the redemption tokens", input: "hello there",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{user} said {input} for {reward} ({cost})"}`),
		want:   []module.Output{rewardChat("CoolViewer said hello there for Say hi (500)")}},
	{name: "chat action falls back to the default template",
		config: rewardConfig(`{"id":"rw-1","action":"chat"}`),
		want:   []module.Output{rewardChat("CoolViewer redeemed Say hi!")}},
	{name: "an announce action runs nothing", config: rewardConfig(`{"id":"rw-1","action":"announce","message":"hi"}`)},
	{name: "a shoutout action runs nothing", config: rewardConfig(`{"id":"rw-1","action":"shoutout","message":"hi"}`)},
	{name: "an unknown action runs nothing", config: rewardConfig(`{"id":"rw-1","action":"bogus","message":"hi"}`)},
	{name: "a blank action runs nothing", config: rewardConfig(`{"id":"rw-1","action":"","message":"hi"}`)},
	{name: "fulfill chats then marks the redemption fulfilled",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"hi","onRedeem":"fulfill"}`),
		want:   []module.Output{rewardChat("hi"), rewardUpdate(outgress.RedemptionFulfilled)}},
	{name: "cancel refunds the redemption",
		config: rewardConfig(`{"id":"rw-1","action":"none","onRedeem":"cancel"}`),
		want:   []module.Output{rewardUpdate(outgress.RedemptionCanceled)}},
	{name: "leave chats and leaves the redemption for a mod",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"hi","onRedeem":"leave"}`),
		want:   []module.Output{rewardChat("hi")}},
	{name: "a choice token resolves to its only option", input: "x",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{choice:only} pick, @{user}"}`),
		want:   []module.Output{rewardChat("only pick, @CoolViewer")}},
	{name: "an empty choice resolves to nothing",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"[{choice:}]"}`),
		want:   []module.Output{rewardChat("[]")}},
	{name: "a bare choice stays literal",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{choice}"}`),
		want:   []module.Output{rewardChat("{choice}")}},
	{name: "a degenerate random range resolves to its bound",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{random:7-7}"}`),
		want:   []module.Output{rewardChat("7")}},
	{name: "an unknown token stays literal",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{unknown}"}`),
		want:   []module.Output{rewardChat("{unknown}")}},
	{name: "leading slashes and spaces never reach the reply", input: " /announce pwned",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"[{input}]"}`),
		want:   []module.Output{rewardChat("[announce pwned]")}},
	{name: "points are awarded to the redeemer",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{user} bought {points} points!","points":250}`),
		want:   []module.Output{rewardChat("CoolViewer bought 250 points!")},
		earns:  []earnCall{{2, 9, "coolviewer", "CoolViewer", 250, 0}}},
	{name: "a counter binding bumps and reports the new value",
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{user} death #{counter}","counter":"deaths"}`),
		want:   []module.Output{rewardChat("CoolViewer death #1")},
		bumps:  []bumpCall{{2, "deaths", 9, "Say hi", 1}}},
	{name: "a live only reward grants nothing while offline but still chats", live: liveOffline,
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{user} +1","counter":"deaths","points":50,"liveOnly":true}`),
		want:   []module.Output{rewardChat("CoolViewer +1")}},
	{name: "a live only reward grants while live", live: liveOnline,
		config: rewardConfig(`{"id":"rw-1","action":"chat","message":"{user} +1","counter":"deaths","points":50,"liveOnly":true}`),
		want:   []module.Output{rewardChat("CoolViewer +1")},
		earns:  []earnCall{{2, 9, "coolviewer", "CoolViewer", 50, 0}},
		bumps:  []bumpCall{{2, "deaths", 9, "Say hi", 1}}},
	{name: "TestChannelPointsStreamerPointsPreference streamer default", userID: "2", loyalty: `{}`,
		config: rewardConfig(`{"id":"rw-1","action":"none","points":250}`),
		earns:  []earnCall{{2, 2, "coolviewer", "CoolViewer", 250, 0}}},
	{name: "TestChannelPointsStreamerPointsPreference streamer off", userID: "2", loyalty: `{"streamerPoints":-1}`,
		config: rewardConfig(`{"id":"rw-1","action":"none","points":250}`)},
	{name: "TestChannelPointsStreamerPointsPreference viewer while streamer off", userID: "7", loyalty: `{"streamerPoints":-1}`,
		config: rewardConfig(`{"id":"rw-1","action":"none","points":250}`),
		earns:  []earnCall{{2, 7, "coolviewer", "CoolViewer", 250, 0}}},
}

func TestChannelPointsRedemptions(t *testing.T) {
	cases := rewardCases
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLoyalty{}
			proj := &fakeProj{}
			if tc.loyalty != "" {
				proj.modules = []projection.ModuleView{{Name: engine.LoyaltyModuleName, IsEnabled: true, Configs: []byte(tc.loyalty)}}
			}
			d := engine.Deps{Loyalty: fake, Live: tc.live.store(), Proj: proj, Log: zap.NewNop()}
			userID := tc.userID
			if userID == "" {
				userID = "9"
			}
			c := eventCtx(eventInput{redemptionAddType, redemptionPayload(userID, tc.input), tc.config})
			assert.Equal(t, tc.want, runEvent(t, ChannelPoints(d), c))
			assert.Equal(t, tc.earns, fake.earns)
			assert.Equal(t, tc.bumps, fake.bumps)
		})
	}
}
