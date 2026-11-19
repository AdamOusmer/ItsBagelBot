// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationSideEffectsAreDeduplicated(t *testing.T) {
	fx := newAutoFixture(manual("main", act("points_add", obj{"login": "target", "amount": 5})))
	first := fx.run(t, "main", "req-points", nil)
	second := fx.run(t, "main", "req-points", nil)
	assert.Equal(t, []string{"0:points_add:ok:"}, statuses(first))
	assert.Equal(t, []string{"0:points_add:skipped:duplicate"}, statuses(second))
	assert.Equal(t, 1, fx.loyalty.adjustCount())

	fx.run(t, "main", "req-points-other", nil)
	assert.Equal(t, 2, fx.loyalty.adjustCount())
	assert.Equal(t, BalanceAdjustment{BroadcasterID: 1, ViewerID: 99, ViewerLogin: "target", Value: 5}, fx.loyalty.adjusts[0])
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
	assert.Equal(t, CounterBump{BroadcasterID: 1, Name: "wins", Viewer: Viewer{ID: 1}, Command: "main", Delta: 2}, fx.loyalty.bumps[0])
	assert.Equal(t, []string{"wins/1/9"}, fx.loyalty.sets)
}

type scopedRecipeCounter struct {
	autoLoyalty
	scope   counterScope
	entries map[resolvedBump]int64
}

func (l *scopedRecipeCounter) bucket(viewer uint64, command string) resolvedBump {
	scope, id, cmd := bumpTarget(l.scope, viewer, command)
	return resolvedBump{scope: scope, viewerID: id, command: cmd}
}

func (l *scopedRecipeCounter) CounterBump(_ context.Context, b CounterBump) (int64, error) {
	key := l.bucket(b.Viewer.ID, b.Command)
	l.entries[key] += b.Delta
	return l.entries[key], nil
}

func (l *scopedRecipeCounter) CounterSet(_ context.Context, set CounterAssignment) (bool, error) {
	key := l.bucket(set.ViewerID, set.Command)
	if key.scope == l.scope {
		l.entries[key] = set.Value
		return true, nil
	}
	if set.RequireTarget {
		return false, bus.RPCReplyError{Message: loyaltyrpc.CounterTargetRequired}
	}
	clear(l.entries)
	return true, nil
}

func (l *scopedRecipeCounter) CounterPeek(_ context.Context, target CounterTarget) (loyaltyrpc.Counter, bool, error) {
	value := l.entries[l.bucket(target.ViewerID, target.Command)]
	return loyaltyrpc.Counter{Value: value, Scope: string(l.scope)}, true, nil
}

func TestProcessRecipeCounterWritesRespectScope(t *testing.T) {
	viewer := resolvedBump{scope: data.CounterScopeViewer, viewerID: 7}
	commandBucket := resolvedBump{scope: data.CounterScopeCommand, command: "id-counter"}
	cases := []struct {
		name    string
		scope   counterScope
		actions []automation.Action
		seed    map[resolvedBump]int64
		entries map[resolvedBump]int64
		steps   []string
		output  []string
	}{
		{
			name: "add reads back from recipe command", scope: data.CounterScopeCommand,
			actions: []automation.Action{act("counter_add", obj{"name": "x", "amount": 7}), chat("{counter:x}")},
			seed:    map[resolvedBump]int64{}, entries: map[resolvedBump]int64{commandBucket: 7},
			steps: []string{"0:counter_add:ok:", "1:chat:ok:"}, output: []string{`chat {"broadcaster_id":"123","message":"7"}`},
		},
		{
			name: "set writes the recipe command", scope: data.CounterScopeCommand,
			actions: []automation.Action{act("counter_set", obj{"name": "x", "value": 12})},
			seed:    map[resolvedBump]int64{commandBucket: 7}, entries: map[resolvedBump]int64{commandBucket: 12},
			steps: []string{"0:counter_set:ok:"},
		},
		{
			name: "viewerless set preserves viewer entries", scope: data.CounterScopeViewer,
			actions: []automation.Action{act("counter_set", obj{"name": "x", "value": 12})},
			seed:    map[resolvedBump]int64{viewer: 5}, entries: map[resolvedBump]int64{viewer: 5},
			steps: []string{"0:counter_set:failed:no_viewer"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := command("counter", obj{"name": "!counter"})
			a.Definition.Actions = tc.actions
			loyalty := &scopedRecipeCounter{scope: tc.scope, entries: tc.seed}
			pub := &fakePublisher{}
			dedup := NewEventDedup(newRecordingStore(), "counter:", time.Minute, nil)
			p, runs := autoRig{automations: []automation.Automation{a}, loyalty: loyalty, pub: pub, dedup: dedup}.pipeline()
			t.Cleanup(p.Close)
			env := chatEnv("!counter", "")
			env.ChatterUserID, env.ChatterUserLogin = "", ""
			env.MsgID = "counter-message"
			msg := bus.NewMessage("counter-test", rawJSON(env))
			require.NoError(t, p.Process(msg))
			awaitCompletion(t, msg, true)
			require.Len(t, runs.recs, 1)
			got := [2][]string{statuses(runs.recs[0]), (&autoFixture{pub: pub}).described(t)}
			assert.Equal(t, [2][]string{tc.steps, tc.output}, got)
			assert.Equal(t, tc.entries, loyalty.entries)
		})
	}
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

func TestAutomationLightsGroupSendsEveryLight(t *testing.T) {
	params := lightsParams()
	params["extra_lights"] = []string{"CC:DD/H6159", "EE:FF/H6008"}
	fx := newAutoFixture(manual("main", act("lights", params)))
	rec := fx.run(t, "main", "req-lights-group", nil)
	assert.Equal(t, []string{"0:lights:ok:"}, statuses(rec))
	assert.Equal(t, []gossiprpc.GoveeLight{{Device: "AA:BB", SKU: "H6008"}, {Device: "CC:DD", SKU: "H6159"}, {Device: "EE:FF", SKU: "H6008"}}, fx.gossip.req.Lights)
}

func TestAutomationLightsGroupSkipsMalformedRefs(t *testing.T) {
	params := lightsParams()
	params["extra_lights"] = []string{"no-sku", "CC:DD/H6159"}
	fx := newAutoFixture(manual("main", act("lights", params)))
	fx.run(t, "main", "req-lights-malformed", nil)
	assert.Equal(t, []gossiprpc.GoveeLight{{Device: "AA:BB", SKU: "H6008"}, {Device: "CC:DD", SKU: "H6159"}}, fx.gossip.req.Lights)
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

func TestAutomationLightsLiveGate(t *testing.T) {
	cases := []struct {
		name    string
		live    fakeLive
		anyTime bool
		want    string
		calls   int
	}{
		{"offline default skips", fakeLive{}, false, "0:lights:skipped:offline", 0},
		{"offline any time runs", fakeLive{}, true, "0:lights:ok:", 1},
		{"live default runs", fakeLive{live: true}, false, "0:lights:ok:", 1},
		{"live lookup failure", fakeLive{err: errors.New("down")}, false, "0:lights:failed:", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := lightsParams()
			if tc.anyTime {
				params["any_time"] = true
			}
			fx := newAutoFixture(manual("main", act("lights", params), act("chat", obj{"text": "after"})))
			fx.p.live = tc.live
			rec := fx.run(t, "main", "req-live-"+tc.name, nil)
			got := statuses(rec)
			require.Len(t, got, 2)
			assert.True(t, strings.HasPrefix(got[0], tc.want), got[0])
			assert.Equal(t, "1:chat:ok:", got[1])
			assert.Equal(t, tc.calls, fx.gossip.calls)
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
