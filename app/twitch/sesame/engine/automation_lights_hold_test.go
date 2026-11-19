// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"testing/synctest"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func heldLights(seconds int) automation.Action {
	params := lightsParams()
	params["hold_s"] = seconds
	params["extra_lights"] = []string{"AA:BB/H0000", "CC:DD/H6159", "CC:DD/H0000"}
	return act("lights", params)
}

type holdCheckpoint struct {
	at    time.Duration
	calls int
}

type holdTimelineCase struct {
	name         string
	controlError string
	offline      bool
	replay       bool
	replies      []gossiprpc.GoveeRestoreReply
	checkpoints  []holdCheckpoint
}

func TestAutomationLightsHoldTimeline(t *testing.T) {
	cases := []holdTimelineCase{
		{
			name:        "restore once despite replay",
			replay:      true,
			checkpoints: []holdCheckpoint{{0, 1}, {4999 * time.Millisecond, 1}, {5010 * time.Millisecond, 2}},
		},
		{
			name:        "retry after rate limit",
			replies:     []gossiprpc.GoveeRestoreReply{{RateLimited: true, RetryAfterMS: 7500}},
			checkpoints: []holdCheckpoint{{0, 1}, {5010 * time.Millisecond, 2}, {12499 * time.Millisecond, 2}, {12510 * time.Millisecond, 3}},
		},
		{
			name:        "retry with default wait",
			replies:     []gossiprpc.GoveeRestoreReply{{RateLimited: true}},
			checkpoints: []holdCheckpoint{{0, 1}, {5010 * time.Millisecond, 2}, {5999 * time.Millisecond, 2}, {6010 * time.Millisecond, 3}},
		},
		{name: "offline skips control and restore", offline: true, checkpoints: []holdCheckpoint{{0, 0}, {5 * time.Second, 0}}},
		{name: "refused control skips restore", controlError: "no key", checkpoints: []holdCheckpoint{{0, 1}, {5 * time.Second, 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, tc.run)
		})
	}
}

func (tc holdTimelineCase) run(t *testing.T) {
	fx := newAutoFixture(manual("main", heldLights(5), chat("now")))
	fx.p.live = fakeLive{live: !tc.offline}
	fx.gossip.controlError, fx.gossip.restoreReplies = tc.controlError, tc.replies
	start := time.Now()
	invokeLightsHold(t, fx.p, "same-run")
	assert.Less(t, time.Since(start), 10*time.Millisecond)
	assert.Equal(t, []string{`chat {"broadcaster_id":"1","message":"now"}`}, fx.described(t))
	fx.p.live = fakeLive{}
	want := checkHoldTimeline(t, fx.gossip, start, tc.checkpoints)
	if tc.replay {
		fx.p.live = liveAlways{}
		invokeLightsHold(t, fx.p, "same-run")
	}
	time.Sleep(time.Minute)
	synctest.Wait()
	assert.Equal(t, want, fx.gossip.lightsCalls())
}

func invokeLightsHold(t *testing.T, p *Pipeline, requestID string) {
	t.Helper()
	reply := p.RunNamed(t.Context(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "main", RequestID: requestID})
	require.True(t, reply.Accepted, reply.Refusal)
}

func checkHoldTimeline(t *testing.T, g *autoGossip, start time.Time, checkpoints []holdCheckpoint) []goveeCall {
	t.Helper()
	var want []goveeCall
	for _, check := range checkpoints {
		at := start.Add(check.at)
		time.Sleep(time.Until(at))
		synctest.Wait()
		got := g.lightsCalls()
		if !assert.Len(t, got, check.calls, "at %s", check.at) {
			continue
		}
		if check.calls > len(want) {
			want = append(want, holdCallAt(t, at, got))
		}
		assert.Equal(t, want, got)
	}
	return want
}

func holdCallAt(t *testing.T, at time.Time, calls []goveeCall) goveeCall {
	t.Helper()
	holdID := calls[0].req.HoldID
	assert.NotEmpty(t, holdID)
	req := gossiprpc.Request{
		ChannelID: "1", Device: "AA:BB", SKU: "H6008", HoldID: holdID,
		Lights: []gossiprpc.GoveeLight{{Device: "AA:BB", SKU: "H6008"}, {Device: "CC:DD", SKU: "H6159"}},
	}
	endpoint := "restore"
	if len(calls) == 1 {
		endpoint = "control"
		req.HoldS, req.ColorRGB = 5, 0xf5a524
	}
	calledAt := calls[len(calls)-1].at
	assert.WithinDuration(t, at, calledAt, 10*time.Millisecond)
	return goveeCall{at: calledAt, route: GossipRoute{Provider: "govee", Endpoint: endpoint}, req: req}
}

func TestAutomationLightsHoldKeepsOuterDelayBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		defer drainHoldEffects()
		fx := newAutoFixture(manual("main", delayMS(59000), heldLights(60), delayMS(1000), chat("after outer delay")))
		invokeLightsHold(t, fx.p, "budget")
		time.Sleep(59010 * time.Millisecond)
		synctest.Wait()
		assert.Len(t, fx.gossip.lightsCalls(), 1)
		assert.Empty(t, fx.described(t))

		time.Sleep(time.Second)
		synctest.Wait()
		assert.Equal(t, []string{`chat {"broadcaster_id":"1","message":"after outer delay"}`}, fx.described(t))

		time.Sleep(59 * time.Second)
		synctest.Wait()
		require.Len(t, fx.gossip.lightsCalls(), 2)
	})
}

func TestAutomationLightsHoldIDsTrackLoopIterationsAndRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		defer drainHoldEffects()
		loop := act("loop", obj{"times": "2", "actions": []automation.Action{heldLights(5)}})
		fx := newAutoFixture(manual("main", loop))
		for _, requestID := range []string{"first", "second", "", ""} {
			invokeLightsHold(t, fx.p, requestID)
		}
		time.Sleep(5010 * time.Millisecond)
		synctest.Wait()
		endpoints := make(map[string][]string)
		for _, call := range fx.gossip.lightsCalls() {
			require.NotEmpty(t, call.req.HoldID)
			endpoints[call.req.HoldID] = append(endpoints[call.req.HoldID], call.route.Endpoint)
		}
		require.Len(t, endpoints, 8)
		for _, calls := range endpoints {
			assert.Equal(t, []string{"control", "restore"}, calls)
		}
	})
}

func drainHoldEffects() {
	time.Sleep(effectLeaseTTL)
	synctest.Wait()
}

func TestDurableLightsHoldResumesOnAnotherReplica(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{}, manual("main", heldLights(5), chat("now")))
	original, resumed := &autoGossip{}, &autoGossip{}
	rig.replicas[0].gossip, rig.replicas[1].gossip = original, resumed
	runID := rig.fire(t, 0, "main", "")
	require.Equal(t, []string{"now"}, rig.awaitTextCount(t, 1))
	control := original.lightsCalls()
	require.Len(t, control, 1)
	rig.kill(0)

	rig.awaitOutcomes(t, runID, 2)
	require.Equal(t, control, original.lightsCalls())
	restore := resumed.lightsCalls()
	require.Len(t, restore, 1)
	assert.Equal(t, GossipRoute{Provider: "govee", Endpoint: "restore"}, restore[0].route)
	assert.Equal(t, gossiprpc.Request{
		ChannelID: control[0].req.ChannelID, Device: control[0].req.Device, SKU: control[0].req.SKU,
		HoldID: control[0].req.HoldID, Lights: control[0].req.Lights,
	}, restore[0].req)
	rig.assertDrained(t)
}
