// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"fmt"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const goveeCfg = `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159"}`

func goveePayload(rewardID, input string) string {
	return fmt.Sprintf(`{"id":"redeem-1","broadcaster_user_id":"2","user_name":"CoolViewer","user_login":"coolviewer","user_input":%q,"reward":{"id":%q,"title":"Colour my lights","cost":500}}`, input, rewardID)
}

func goveeGossip(err error) *fakeGossip {
	return &fakeGossip{err: err, replies: map[string]any{"govee.control": gossiprpc.GoveeControlReply{OK: true}}}
}

func runGovee(t *testing.T, d engine.Deps, c *module.Context) []module.Output {
	t.Helper()
	d.Log = zap.NewNop()
	return runEvent(t, Govee(d), c)
}

func outputKinds(out []module.Output) []string {
	var kinds []string
	for _, o := range out {
		if o.Type == outgress.TypeRedemptionUpdate {
			kinds = append(kinds, "update:"+o.Status)
			continue
		}
		kinds = append(kinds, string(o.Type))
	}
	return kinds
}

func TestGoveeRedemptions(t *testing.T) {
	const (
		fulfilled = "update:" + outgress.RedemptionFulfilled
		canceled  = "update:" + outgress.RedemptionCanceled
		chat      = string(outgress.TypeChat)
	)
	blue := gossiprpc.Request{ChannelID: "2", Device: "AB:CD:EF", SKU: "H6159", ColorRGB: 0x0066FF}
	cases := []struct {
		name    string
		config  string
		payload string
		live    liveState
		gossip  error
		kinds   []string
		chat    string
		call    *gossiprpc.Request
	}{
		{name: "an unconfigured light does nothing", config: `{"rewardId":"rw-1"}`, live: liveOnline},
		{name: "an unrelated reward never drives the lights", live: liveOnline, payload: goveePayload("other", "blue"), config: goveeCfg},
		{name: "offline refunds without calling gossip", live: liveOffline, config: goveeCfg, kinds: []string{chat, canceled}, chat: "refunded"},
		{name: "an unconfirmed live state refunds instead of driving lights", live: liveBroken, config: goveeCfg, kinds: []string{chat, canceled}, chat: "refunded"},
		{name: "an unknown colour refunds before gossip", live: liveOnline, payload: goveePayload("rw-1", "chartreuseish"), config: goveeCfg,
			kinds: []string{chat, canceled}, chat: "refunded"},
		{name: "a known colour drives the light and fulfills", live: liveOnline, config: goveeCfg, kinds: []string{chat, fulfilled}, chat: "@CoolViewer", call: &blue},
		{name: "allowOffline drives the light while offline", live: liveOffline,
			config: `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","allowOffline":true}`, kinds: []string{chat, fulfilled}, call: &blue},
		{name: "a gossip failure refunds with the provider reason", live: liveOnline, gossip: bus.RPCReplyError{Message: "too many light changes, slow down"},
			config: goveeCfg, kinds: []string{chat, canceled}, chat: "too many light changes", call: &blue},
		{name: "the leave policy chats without resolving the redemption", live: liveOnline,
			config: `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","onRedeem":"leave"}`, kinds: []string{chat}, call: &blue},
		{name: "off powers the light off when allowed", live: liveOnline, payload: goveePayload("rw-1", "off"),
			config: `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","allowOff":true}`, kinds: []string{chat, fulfilled},
			call: &gossiprpc.Request{ChannelID: "2", Device: "AB:CD:EF", SKU: "H6159", PowerOff: true}},
		{name: "off refunds when the action is disabled", live: liveOnline, payload: goveePayload("rw-1", "off"), config: goveeCfg,
			kinds: []string{chat, canceled}, chat: "refunded"},
		{name: "a custom reply fills the redemption tokens", live: liveOnline,
			config: `{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","replyMessage":"{user} painted the room {color}"}`,
			kinds:  []string{chat, fulfilled}, chat: "CoolViewer painted the room blue", call: &blue},
		{name: "a multi binding config drives the redeemed reward's light", live: liveOnline, payload: goveePayload("rw-2", "red"),
			config: `{"bindings":[{"rewardId":"rw-1","device":"AA:AA:AA","sku":"H1"},{"rewardId":"rw-2","device":"BB:BB:BB","sku":"H2"}]}`,
			kinds:  []string{chat, fulfilled}, call: &gossiprpc.Request{ChannelID: "2", Device: "BB:BB:BB", SKU: "H2", ColorRGB: 0xFF0000}},
		{name: "a multi binding config ignores an unbound reward", live: liveOnline, payload: goveePayload("other", "red"),
			config: `{"bindings":[{"rewardId":"rw-1","device":"AA:AA:AA","sku":"H1"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := goveeGossip(tc.gossip)
			payload := tc.payload
			if payload == "" {
				payload = goveePayload("rw-1", "blue")
			}
			out := runGovee(t, engine.Deps{Live: tc.live.store(), Gossip: gw}, eventCtx(eventInput{redemptionAddType, payload, tc.config}))
			assert.Equal(t, tc.kinds, outputKinds(out))
			if tc.chat != "" {
				require.NotEmpty(t, out)
				assert.Contains(t, out[0].Text, tc.chat)
			}
			if tc.call == nil {
				assert.Empty(t, gw.calls, "no light may be driven")
				return
			}
			require.Len(t, gw.calls, 1)
			assert.Equal(t, "govee", gw.calls[0].provider)
			assert.Equal(t, "control", gw.calls[0].endpoint)
			assert.Equal(t, *tc.call, gw.calls[0].req)
		})
	}
}

func TestGoveeReplyTemplates(t *testing.T) {
	cases := []struct{ template, want string }{
		{"{choice:only} light, @{user}", "only light, @CoolViewer"},
		{"[{choice:}]", "[]"},
		{"{choice}", "{choice}"},
		{"{random:7-7}", "7"},
		{"{unknown}", "{unknown}"},
		{"@{user} set the lights to {color}!", "@CoolViewer set the lights to blue!"},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			cfg := fmt.Sprintf(`{"rewardId":"rw-1","device":"AB:CD:EF","sku":"H6159","replyMessage":%q}`, tc.template)
			d := engine.Deps{Live: &fakeLive{live: true}, Gossip: goveeGossip(nil)}
			out := runGovee(t, d, eventCtx(eventInput{redemptionAddType, goveePayload("rw-1", "blue"), cfg}))
			require.NotEmpty(t, out)
			assert.Equal(t, tc.want, out[0].Text)
		})
	}
}

func TestGoveeColourInput(t *testing.T) {
	cases := []struct {
		input string
		rgb   int
		ok    bool
	}{
		{"red", 0xFF0000, true},
		{"BLUE", 0x0066FF, true},
		{" green ", 0x00C000, true},
		{"magenta", 0xFF00FF, true},
		{"white", 0xFFFFFF, true},
		{"#00ccff", 0x00CCFF, true},
		{"00ccff", 0x00CCFF, true},
		{"#FFF", 0xFFFFFF, true},
		{"f80", 0xFF8800, true},
		{"#000000", 0x000000, true},
		{"", 0, false},
		{"   ", 0, false},
		{"notacolor", 0, false},
		{"#12", 0, false},
		{"12345", 0, false},
		{"#gggggg", 0, false},
		{"#1234567", 0, false},
		{"rgb(1,2,3)", 0, false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			gw := goveeGossip(nil)
			d := engine.Deps{Live: &fakeLive{live: true}, Gossip: gw}
			out := runGovee(t, d, eventCtx(eventInput{redemptionAddType, goveePayload("rw-1", tc.input), goveeCfg}))
			if !tc.ok {
				assertRefund(t, out)
				assert.Empty(t, gw.calls, "a bad colour must refund before gossip")
				return
			}
			require.Len(t, gw.calls, 1)
			assert.Equal(t, tc.rgb, gw.calls[0].req.ColorRGB)
			assert.False(t, gw.calls[0].req.PowerOff)
			assert.Equal(t, []string{"chat", "update:" + outgress.RedemptionFulfilled}, outputKinds(out))
		})
	}
}
