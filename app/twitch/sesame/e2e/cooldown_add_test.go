// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	namedCooldownName = "subathon"
	addAutomationID   = "add-sub"
)

// addTrigger names the event cooldown_add fires on, and the per/seconds it extends by. Both worked examples from
// the brief share everything except these: a sub filters is_gift=false and adds a flat amount; a gift fires on
// every gift with per: 1, so the added time scales with the gift count on the event.
type addTrigger struct {
	kind    string
	filters map[string]string
	seconds int
	per     int
}

// namedCooldownRecipes builds an "!arm" recipe owning a one-hour named window, answering its time left on
// leftCommand, and a recipe triggered by trig that extends it.
func namedCooldownRecipes(t *testing.T, channel uint64, trig addTrigger) (arm, add automation.Automation) {
	t.Helper()
	condParams, err := codec.Marshal(step{"global_s": oneHourSeconds, "name": namedCooldownName, "left_command": leftCommand})
	require.NoError(t, err)
	arm = recipeWith(t, channel, "!arm", chatStep("armed"))
	arm.Definition.Conditions = []automation.Condition{{Type: "cooldown", Params: condParams}}
	require.Empty(t, automation.Validate(arm.Definition))

	addParams := step{"name": namedCooldownName, "seconds": trig.seconds}
	if trig.per > 0 {
		addParams["per"] = trig.per
	}
	triggerParams := step{"kind": trig.kind}
	if trig.filters != nil {
		triggerParams["filters"] = trig.filters
	}
	raw, err := codec.Marshal(step{
		"version": 1,
		"trigger": step{"type": "event", "params": triggerParams},
		"actions": []step{{"type": "cooldown_add", "params": addParams}},
	})
	require.NoError(t, err)
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(raw, &def))
	require.Empty(t, automation.Validate(def))
	add = automation.Automation{ID: addAutomationID, UserID: channel, Name: addAutomationID, Enabled: true, Definition: def}
	return arm, add
}

// dispatchAt names where one event dispatch through the rig lands: which replica saw it, its redelivery
// identity, and which viewer it came from.
type dispatchAt struct {
	replica int
	eventID string
	viewer  uint64
}

func (r *cooldownRig) dispatch(t *testing.T, at dispatchAt, kind, body string) {
	t.Helper()
	r.process(t, at.replica, lane.Envelope{
		Type: kind, Lane: "premium", EventID: at.eventID,
		BroadcasterUserID: strconv.FormatUint(r.channel, 10), BroadcasterUserLogin: "streamer",
		Event: codec.RawMessage(body),
	})
}

func subscribeEvent(viewer uint64, isGift bool) string {
	id := strconv.FormatUint(viewer, 10)
	return `{"user_id":"` + id + `","user_login":"viewer` + id + `","user_name":"Viewer` + id +
		`","tier":"1000","is_gift":"` + strconv.FormatBool(isGift) + `"}`
}

func (r *cooldownRig) subscribe(t *testing.T, at dispatchAt, isGift bool) {
	t.Helper()
	r.dispatch(t, at, "channel.subscribe", subscribeEvent(at.viewer, isGift))
}

func (r *cooldownRig) runArm(t *testing.T, at dispatchAt) {
	t.Helper()
	id := strconv.FormatUint(at.viewer, 10)
	r.process(t, at.replica, lane.Envelope{
		Type: "channel.chat.message", Lane: "premium", EventID: at.eventID, Text: "!arm",
		BroadcasterUserID: strconv.FormatUint(r.channel, 10), BroadcasterUserLogin: "streamer",
		ChatterUserID: id, ChatterUserLogin: "viewer" + id,
	})
}

func giftEvent(viewer uint64, count int) string {
	id := strconv.FormatUint(viewer, 10)
	return `{"user_id":"` + id + `","user_login":"viewer` + id + `","user_name":"Viewer` + id +
		`","tier":"1000","total":` + strconv.Itoa(count) + `,"cumulative_total":` + strconv.Itoa(count) + `,"is_anonymous":"false"}`
}

func (r *cooldownRig) gift(t *testing.T, at dispatchAt, count int) {
	t.Helper()
	r.dispatch(t, at, "channel.subscription.gift", giftEvent(at.viewer, count))
}

func TestCooldownAddOnAGiftSubScalesWithTheGiftCount(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_add_giftrate_"), Dialect: "mysql"})
	r.channel = uniqueChannel(45)
	arm, add := namedCooldownRecipes(t, r.channel, addTrigger{kind: "channel.subscription.gift", seconds: 30, per: 1})
	r.source.set(r.channel, arm, add)

	r.runArm(t, dispatchAt{eventID: r.nextEvent("arm"), viewer: alice})
	require.Equal(t, automationrpc.OutcomeOK, r.awaitAutomationRun(t, "!arm", 1).Outcome)
	before := r.storedWindows(t)
	require.Len(t, before, 1)

	r.gift(t, dispatchAt{replica: 1, eventID: r.nextEvent("gift"), viewer: carol}, 5)
	added := r.awaitAutomationRun(t, addAutomationID, 1)
	require.Equal(t, automationrpc.OutcomeOK, added.Outcome, statusesOf(added))
	assert.Equal(t, "150s", added.Actions[0].Detail, "5 gifts at 30s each")

	after := r.storedWindows(t)
	require.Len(t, after, 1)
	assert.Equal(t, before[0]+150*1000, after[0])
}

func (r *cooldownRig) awaitAutomationRun(t *testing.T, automationID string, atLeast int) automationrpc.RunRecord {
	t.Helper()
	var latest automationrpc.RunRecord
	require.Eventually(t, func() bool {
		runs, err := r.runs.Recent(context.Background(), r.channel, automationID, automation.RunsKept)
		if err != nil || len(runs) < atLeast {
			return false
		}
		latest = runs[0]
		return true
	}, settle, 20*time.Millisecond)
	return latest
}

func statusesOf(rec automationrpc.RunRecord) []string {
	out := make([]string, len(rec.Actions))
	for i, a := range rec.Actions {
		out[i] = a.Type + ":" + a.Status + ":" + a.Detail
	}
	return out
}

func TestNamedCooldownExtendedPastADayStillShowsTheRightTimeLeft(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_add_"), Dialect: "mysql"})
	r.channel = uniqueChannel(40)
	arm, add := namedCooldownRecipes(t, r.channel, addTrigger{kind: "channel.subscribe", filters: map[string]string{"is_gift": "false"}, seconds: 23 * 3600})
	r.source.set(r.channel, arm, add)

	r.runArm(t, dispatchAt{eventID: r.nextEvent("arm"), viewer: alice})
	armed := r.awaitAutomationRun(t, "!arm", 1)
	require.Equal(t, automationrpc.OutcomeOK, armed.Outcome, "the window was free the first time")

	before := r.storedWindows(t)
	require.Len(t, before, 1)
	assert.InDelta(t, time.Now().Add(oneHourSeconds*time.Second).UnixMilli(), before[0], 60_000, "one hour, as the condition asked")
	assert.Regexp(t, "^Viewer102, "+oneHourLeft+waitingSuffix, r.peek(t, bob))

	r.subscribe(t, dispatchAt{replica: 1, eventID: r.nextEvent("sub"), viewer: carol}, false)
	added := r.awaitAutomationRun(t, addAutomationID, 1)
	require.Equal(t, automationrpc.OutcomeOK, added.Outcome, statusesOf(added))
	require.Equal(t, "82800s", added.Actions[0].Detail)

	after := r.storedWindows(t)
	require.Len(t, after, 1, "the row is still the only copy of this window")
	assert.Equal(t, before[0]+23*3600*1000, after[0], "23 hours landed exactly, taking the window past the 24-hour mark")

	assert.Regexp(t, "^Viewer102, (1d|23h).*"+waitingSuffix, r.peek(t, bob), "time left shows the extended window, past a day")

	r.runArm(t, dispatchAt{eventID: r.nextEvent("arm2"), viewer: carol})
	refused := r.awaitAutomationRun(t, "!arm", 2)
	assert.Equal(t, automationrpc.OutcomeSkipped, refused.Outcome, "the extended window is still running")
}

func TestCooldownAddIgnoresAGiftSubAndFiresOnlyForTheGiftRecipe(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_add_gift_"), Dialect: "mysql"})
	r.channel = uniqueChannel(43)
	arm, add := namedCooldownRecipes(t, r.channel, addTrigger{kind: "channel.subscribe", filters: map[string]string{"is_gift": "false"}, seconds: 60})
	r.source.set(r.channel, arm, add)

	r.runArm(t, dispatchAt{eventID: r.nextEvent("arm"), viewer: alice})
	require.Equal(t, automationrpc.OutcomeOK, r.awaitAutomationRun(t, "!arm", 1).Outcome)
	before := r.storedWindows(t)
	require.Len(t, before, 1)

	r.subscribe(t, dispatchAt{eventID: r.nextEvent("gift-sub"), viewer: bob}, true)
	require.NoError(t, r.nc.Flush())
	time.Sleep(150 * time.Millisecond)

	after := r.storedWindows(t)
	assert.Equal(t, before, after, "a gift sub does not match is_gift=false and never extends the window")
}

// A redelivery of the same event, landing on a different replica the way NATS can resend it, must add the
// seconds at most once: the dedup claim is keyed off the event identity, not off which process saw it.
func TestCooldownAddRedeliveryAcrossReplicasAddsOnlyOnce(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_add_redelivery_"), Dialect: "mysql"})
	r.channel = uniqueChannel(44)
	arm, add := namedCooldownRecipes(t, r.channel, addTrigger{kind: "channel.subscribe", filters: map[string]string{"is_gift": "false"}, seconds: 30})
	r.source.set(r.channel, arm, add)

	r.runArm(t, dispatchAt{eventID: r.nextEvent("arm"), viewer: alice})
	require.Equal(t, automationrpc.OutcomeOK, r.awaitAutomationRun(t, "!arm", 1).Outcome)
	before := r.storedWindows(t)
	require.Len(t, before, 1)

	redelivered := r.nextEvent("redelivered-sub")
	r.subscribe(t, dispatchAt{eventID: redelivered, viewer: bob}, false)
	first := r.awaitAutomationRun(t, addAutomationID, 1)
	require.Equal(t, automationrpc.OutcomeOK, first.Outcome, statusesOf(first))

	r.subscribe(t, dispatchAt{replica: 1, eventID: redelivered, viewer: bob}, false)
	second := r.awaitAutomationRun(t, addAutomationID, 2)
	assert.Equal(t, "duplicate", second.Actions[0].Detail, "the second replica saw the same event identity and skipped")

	after := r.storedWindows(t)
	require.Len(t, after, 1)
	assert.Equal(t, before[0]+30*1000, after[0], "only one of the two deliveries added its 30 seconds")
}
