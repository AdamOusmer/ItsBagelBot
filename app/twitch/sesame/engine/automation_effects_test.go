// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationSideEffectsAreDeduplicated(t *testing.T) {
	fx := newAutoFixture(manual("main", act("points_add", obj{"login": "", "amount": 5})))
	first := fx.run(t, "main", "req-points", nil)
	second := fx.run(t, "main", "req-points", nil)
	assert.Equal(t, []string{"0:points_add:ok:"}, statuses(first))
	assert.Equal(t, []string{"0:points_add:skipped:duplicate"}, statuses(second))
	assert.Equal(t, 1, fx.loyalty.adjustCount())

	fx.run(t, "main", "req-points-other", nil)
	assert.Equal(t, 2, fx.loyalty.adjustCount())
	assert.Equal(t, BalanceAdjustment{BroadcasterID: 1, ViewerID: 1, Value: 5}, fx.loyalty.adjusts[0])
}

func TestAutomationStoreActions(t *testing.T) {
	fx := newAutoFixture(manual("main",
		act("points_set", obj{"login": "@Target", "amount": 40}),
		act("counter_add", obj{"name": "wins", "amount": 2}),
		act("counter_set", obj{"name": "wins", "value": 9}),
	))
	rec := fx.run(t, "main", "req-store", nil)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, BalanceAdjustment{BroadcasterID: 1, ViewerID: 99, ViewerLogin: "target", Value: 40, Absolute: true}, fx.loyalty.adjusts[0])
	assert.Equal(t, CounterBump{BroadcasterID: 1, Name: "wins", Viewer: Viewer{ID: 1}, Delta: 2}, fx.loyalty.bumps[0])
	assert.Equal(t, []string{"wins/1/9"}, fx.loyalty.sets)
}

func TestAutomationHTTPPost(t *testing.T) {
	cases := []struct {
		status gossiprpc.FetchStatus
		want   string
	}{{gossiprpc.FetchOK, "0:http_post:ok:"}, {gossiprpc.FetchDenied, "0:http_post:failed:denied"}}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			fx := newAutoFixture(manual("main", act("http_post", obj{"url": "https://example.com/hook", "body": `value={arg:k}`})))
			fx.gossip.status = tc.status
			rec := fx.run(t, "main", "req-http", map[string]string{"k": "42"})
			assert.Equal(t, []string{tc.want}, statuses(rec))
			assert.Equal(t, GossipRoute{Provider: "custom", Endpoint: "post"}, fx.gossip.route)
			assert.Equal(t, gossiprpc.Request{
				Account: "1", ChannelID: "1", Def: &gossiprpc.FetchDef{URL: "https://example.com/hook"}, Body: `value=42`,
			}, fx.gossip.req)
		})
	}
}

func lightsParams() obj {
	return obj{"device": "AA:BB", "sku": "H6008", "color": "#f5a524"}
}

func TestAutomationLightsWithoutGoveeModule(t *testing.T) {
	fx := newAutoFixture(manual("main", act("lights", lightsParams())))
	rec := fx.run(t, "main", "req-lights", nil)
	assert.Equal(t, []string{"0:lights:ok:"}, statuses(rec))
	assert.Equal(t, GossipRoute{Provider: "govee", Endpoint: "control"}, fx.gossip.route)
	assert.Equal(t, gossiprpc.Request{ChannelID: "1", Device: "AA:BB", SKU: "H6008", ColorRGB: 0xf5a524}, fx.gossip.req)
}

func TestAutomationLightsSceneFailsAgainstAnOutdatedGossip(t *testing.T) {
	scene := obj{"device": "AA:BB", "sku": "H6008", "mode": "scene", "scene_type": "devices.capabilities.dynamic_scene", "scene_instance": "lightScene", "scene_value": "101"}
	cases := []struct {
		name     string
		outdated bool
		want     string
	}{
		{"current gossip", false, "0:lights:ok:"},
		{"gossip that ignores scenes", true, "0:lights:failed:gossip_outdated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act("lights", scene)))
			fx.gossip.outdated = tc.outdated
			rec := fx.run(t, "main", "req-scene", nil)
			assert.Equal(t, []string{tc.want}, statuses(rec))
		})
	}
}

func discordParams(text string) obj {
	return obj{"guild_id": "123456789012345678", "channel_id": "223456789012345678", "text": text}
}

func TestAutomationDiscordPostNamesTheBroadcasterGuildAndChannel(t *testing.T) {
	fx := newAutoFixture(manual("main", act("discord_post", discordParams("hi {arg:who}"))))
	rec := fx.run(t, "main", "req-discord", map[string]string{"who": "chat"})
	assert.Equal(t, []string{"0:discord_post:ok:"}, statuses(rec))
	assert.Equal(t, []outgressrpc.DiscordPostRequest{
		{UserID: "1", GuildID: "123456789012345678", ChannelID: "223456789012345678", Content: "hi chat"},
	}, fx.discord.sent())
}

func TestAutomationDiscordPostFailuresDoNotStopFlow(t *testing.T) {
	cases := []struct {
		name  string
		reply outgressrpc.DiscordPostReply
		err   error
		want  string
	}{
		{"not bound", outgressrpc.DiscordPostReply{Code: outgressrpc.CodeNotBound, Error: "not bound"}, nil, "0:discord_post:failed:not_bound"},
		{"channel elsewhere", outgressrpc.DiscordPostReply{Code: outgressrpc.CodeForbidden}, nil, "0:discord_post:failed:forbidden"},
		{"bot offline", outgressrpc.DiscordPostReply{Code: outgressrpc.CodeDiscordUnavailable}, nil, "0:discord_post:failed:discord_unavailable"},
		{"transport", outgressrpc.DiscordPostReply{}, errors.New("timeout"), "0:discord_post:failed:discord_error"},
		{"outdated outgress", outgressrpc.DiscordPostReply{Code: outgressrpc.CodeOK}, nil, "0:discord_post:failed:outgress_outdated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act("discord_post", discordParams("hi")), act("chat", obj{"text": "after"})))
			fx.discord.reply, fx.discord.err = tc.reply, tc.err
			rec := fx.run(t, "main", "req-"+tc.name, nil)
			assert.Equal(t, []string{tc.want, "1:chat:ok:"}, statuses(rec))
			assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
			assert.Len(t, fx.messages(t), 1)
		})
	}
}

func TestAutomationDiscordPostWithoutAPosterFails(t *testing.T) {
	fx := newAutoFixture(manual("main", act("discord_post", discordParams("hi"))))
	fx.p.discord = nil
	rec := fx.run(t, "main", "req-discord-none", nil)
	assert.Equal(t, []string{"0:discord_post:failed:no_discord"}, statuses(rec))
}

func TestAutomationDiscordPostSkipsAnEmptyMessageAndCapsALongOne(t *testing.T) {
	fx := newAutoFixture(manual("main", act("discord_post", discordParams("{arg:none|}")), act("discord_post", discordParams("{arg:long}"))))
	rec := fx.run(t, "main", "req-discord-text", map[string]string{"long": strings.Repeat("é", 2100)})
	assert.Equal(t, []string{"0:discord_post:skipped:empty", "1:discord_post:ok:"}, statuses(rec))
	sent := fx.discord.sent()
	require.Len(t, sent, 1)
	assert.Equal(t, 2000, len([]rune(sent[0].Content)))
}

func TestAutomationDiscordPostDeduplicated(t *testing.T) {
	fx := newAutoFixture(manual("main", act("discord_post", discordParams("hi"))))
	fx.run(t, "main", "req-discord-dup", nil)
	second := fx.run(t, "main", "req-discord-dup", nil)
	assert.Equal(t, []string{"0:discord_post:skipped:duplicate"}, statuses(second))
	assert.Len(t, fx.discord.sent(), 1)
}

func TestAutomationLightsSceneCarriesTheStoredScene(t *testing.T) {
	fx := newAutoFixture(manual("main", act("lights", obj{
		"device": "AA:BB", "sku": "H6008", "mode": "scene", "scene_id": "snapshot:4:9", "scene_name": "Sunset",
		"scene_type": "devices.capabilities.dynamic_scene", "scene_instance": "snapshot", "scene_value": `{"paramId":4,"id":9}`,
	})))
	rec := fx.run(t, "main", "req-lights-scene-ok", nil)
	assert.Equal(t, []string{"0:lights:ok:"}, statuses(rec))
	assert.Equal(t, GossipRoute{Provider: "govee", Endpoint: "control"}, fx.gossip.route)
	assert.Equal(t, gossiprpc.Request{
		ChannelID: "1", Device: "AA:BB", SKU: "H6008",
		Scene: &gossiprpc.GoveeScene{
			ID: "snapshot:4:9", Name: "Sunset", Type: "devices.capabilities.dynamic_scene",
			Instance: "snapshot", Value: codec.RawMessage(`{"paramId":4,"id":9}`),
		},
	}, fx.gossip.req)
}

func TestAutomationLightsFailuresDoNotStopFlow(t *testing.T) {
	cases := []struct {
		name   string
		params obj
		err    error
		want   string
		calls  int
	}{
		{"no_key", lightsParams(), bus.RPCReplyError{Message: "no Govee API key on file"}, "0:lights:failed:no Govee API key on file", 1},
		{"transport", lightsParams(), errors.New("timeout"), "0:lights:failed:gossip_error", 1},
		{"no_device", obj{"device": "", "sku": "H6008", "color": "#f5a524"}, nil, "0:lights:failed:no_device", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act("lights", tc.params), act("chat", obj{"text": "after"})))
			fx.gossip.err = tc.err
			rec := fx.run(t, "main", "req-"+tc.name, nil)
			assert.Equal(t, []string{tc.want, "1:chat:ok:"}, statuses(rec))
			assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
			assert.Equal(t, tc.calls, fx.gossip.calls)
			assert.Len(t, fx.messages(t), 1)
		})
	}
}

func TestAutomationLightsDeduplicated(t *testing.T) {
	fx := newAutoFixture(manual("main", act("lights", lightsParams())))
	fx.run(t, "main", "req-lights-dup", nil)
	second := fx.run(t, "main", "req-lights-dup", nil)
	assert.Equal(t, []string{"0:lights:skipped:duplicate"}, statuses(second))
	assert.Equal(t, 1, fx.gossip.calls)
}

func TestAutomationLightsStoredSceneIsBadParams(t *testing.T) {
	params := lightsParams()
	params["scene"] = "sunset"
	fx := newAutoFixture(manual("main", act("lights", params)))
	rec := fx.run(t, "main", "req-lights-scene", nil)
	assert.Equal(t, []string{"0:lights:failed:bad_params"}, statuses(rec))
	assert.Zero(t, fx.gossip.calls)
}

func TestAutomationEventRunTemplatesAndEnvelopeActions(t *testing.T) {
	auto := manual("main",
		act("chat", obj{"text": "{user} got {event.reward} from {event.login}: {args}"}),
		act("reply", obj{"text": "thanks"}),
		act("delete_message", obj{}),
		act("timeout", obj{"login": "{user}", "seconds": 5, "reason": "r"}),
		act("redemption", obj{"status": "fulfill"}),
	)
	fx := newAutoFixture(auto)
	run := &AutomationRun{
		Plan: automation.NewPlan(auto), BroadcasterID: 1, Channel: "1", Source: "event", ID: "run-env", Identity: "ident",
		Env: &lane.Envelope{
			BroadcasterUserID: "1", MsgID: "m1", EventID: "e1",
			Event: codec.RawMessage(`{"id":"red-1","reward":{"id":"rw-1"}}`),
		},
		Viewer: Viewer{ID: 7, Login: "alice", Name: "Alice"},
		Args:   "a /b",
		Event:  map[string]string{"reward": "Hydrate", "login": "bob"},
	}
	out := fx.p.detachedEmit(context.Background(), 1, "", "env")
	fx.p.runAutomation(context.Background(), run, out.emit)
	require.NoError(t, fx.p.flushDetached(context.Background(), out))

	assert.Equal(t, automationrpc.OutcomeOK, run.Record.Outcome, statuses(run.Record))
	assert.Equal(t, []string{
		`chat {"broadcaster_id":"1","message":"Alice got Hydrate from bob: a /b"}`,
		`chat {"broadcaster_id":"1","message":"@Alice thanks"}`,
		`delete msg=m1`,
		`timeout {"data":{"user_id":"7","duration":5,"reason":"r"}}`,
		`redemption_update reward=rw-1 redemption=red-1 status=FULFILLED`,
	}, fx.described(t))
}

func TestAutomationEnvelopeActionsFailWithoutEnvelope(t *testing.T) {
	fx := newAutoFixture(manual("main", act("delete_message", obj{}), act("redemption", obj{"status": "cancel"}), act("timeout", obj{"login": "ghost", "seconds": 5, "reason": ""})))
	rec := fx.run(t, "main", "req-noenv", nil)
	assert.Equal(t, []string{"0:delete_message:failed:no_message", "1:redemption:failed:no_redemption", "2:timeout:failed:not_found"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomeFailed, rec.Outcome)
	assert.Empty(t, fx.pub.snapshot())
}
