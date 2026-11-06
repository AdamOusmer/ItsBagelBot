// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func cheerCooldownRecipes(t *testing.T, channel uint64, secondsPerHundredBits int) (arm, add automation.Automation) {
	t.Helper()
	condParams, err := codec.Marshal(step{"global_s": oneHourSeconds, "name": namedCooldownName, "left_command": leftCommand})
	require.NoError(t, err)
	arm = recipeWith(t, channel, "!arm", chatStep("armed"))
	arm.Definition.Conditions = []automation.Condition{{Type: "cooldown", Params: condParams}}
	require.Empty(t, automation.Validate(arm.Definition))

	raw, err := codec.Marshal(step{
		"version": 1,
		"trigger": step{"type": "event", "params": step{"kind": "channel.cheer"}},
		"actions": []step{{"type": "cooldown_add", "params": step{"name": namedCooldownName, "seconds": secondsPerHundredBits, "per": 100}}},
	})
	require.NoError(t, err)
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(raw, &def))
	require.Empty(t, automation.Validate(def))
	add = automation.Automation{ID: addAutomationID, UserID: channel, Name: addAutomationID, Enabled: true, Definition: def}
	return arm, add
}

func cheerEvent(viewer uint64, bits int) string {
	id := strconv.FormatUint(viewer, 10)
	return `{"user_id":"` + id + `","user_login":"viewer` + id + `","user_name":"Viewer` + id +
		`","bits":` + strconv.Itoa(bits) + `,"message":"","is_anonymous":"false"}`
}

func (r *cooldownRig) cheer(t *testing.T, replica int, eventID string, viewer uint64, bits int) {
	t.Helper()
	r.process(t, replica, lane.Envelope{
		Type: "channel.cheer", Lane: "premium", EventID: eventID,
		BroadcasterUserID: strconv.FormatUint(r.channel, 10), BroadcasterUserLogin: "streamer",
		Event: codec.RawMessage(cheerEvent(viewer, bits)),
	})
}

func TestCooldownAddOnBitsCheerScalesWithTheBitCount(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_add_bits_"), Dialect: "mysql"})
	r.channel = uniqueChannel(46)
	arm, add := cheerCooldownRecipes(t, r.channel, 20)
	r.source.set(r.channel, arm, add)

	r.runArm(t, 0, r.nextEvent("arm"), alice)
	require.Equal(t, automationrpc.OutcomeOK, r.awaitAutomationRun(t, "!arm", 1).Outcome)
	before := r.storedWindows(t)
	require.Len(t, before, 1)

	r.cheer(t, 1, r.nextEvent("cheer"), carol, 350)
	added := r.awaitAutomationRun(t, addAutomationID, 1)
	require.Equal(t, automationrpc.OutcomeOK, added.Outcome, statusesOf(added))
	assert.Equal(t, "60s", added.Actions[0].Detail, "floor(350/100)=3 lots of 20s, the 50 leftover bits carry nothing")

	after := r.storedWindows(t)
	require.Len(t, after, 1)
	assert.Equal(t, before[0]+60*1000, after[0])
}

// A viewer cheering below the rate leaves the window exactly as it was: a store call would have nothing to add,
// so cooldown_add never makes one.
func TestCooldownAddOnBitsCheerBelowTheRateAddsNothing(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_add_bits_low_"), Dialect: "mysql"})
	r.channel = uniqueChannel(47)
	arm, add := cheerCooldownRecipes(t, r.channel, 20)
	r.source.set(r.channel, arm, add)

	r.runArm(t, 0, r.nextEvent("arm"), alice)
	require.Equal(t, automationrpc.OutcomeOK, r.awaitAutomationRun(t, "!arm", 1).Outcome)
	before := r.storedWindows(t)
	require.Len(t, before, 1)

	r.cheer(t, 0, r.nextEvent("cheer-low"), bob, 40)
	added := r.awaitAutomationRun(t, addAutomationID, 1)
	assert.Equal(t, automationrpc.OutcomeOK, added.Outcome, statusesOf(added))
	assert.Equal(t, "below_rate", added.Actions[0].Detail)

	assert.Equal(t, before, r.storedWindows(t))
}

// claimAndExtend drives a claim and a batch of concurrent extends through the production path (MarkedLongCooldowns
// over the real modules RPC), the same objects wiring in the worker binary.
func (r *cooldownRig) longStore() engine.LongCooldownStore {
	return engine.NewMarkedLongCooldowns(r.vc, r.vars, zap.NewNop())
}

// A claim and a batch of extends on the same named window, fired at once, must never corrupt it: whichever extends
// land after the claim exists add exactly once each, and none of this ever panics or deadlocks.
func TestClaimVersusConcurrentExtendsNeverCorruptTheWindow(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_claim_extend_race_"), Dialect: "mysql"})
	channel := uniqueChannel(48)
	r.channel = channel
	key := automation.LongCooldownNamedKey(channel, namedCooldownName)
	store := r.longStore()

	const extenders, addEach = 40, int64(5)
	var claimed engine.LongCooldownHold
	var claimWon bool
	var foundCount, errCount atomic64

	var group sync.WaitGroup
	start := make(chan struct{})
	group.Go(func() {
		<-start
		hold, won, err := store.ClaimLongCooldowns(context.Background(), engine.LongCooldownClaim{
			BroadcasterID: channel, Windows: []engine.CooldownWindow{{Key: key, TTL: time.Hour}},
		})
		require.NoError(t, err)
		claimed, claimWon = hold, won
	})
	for range extenders {
		group.Go(func() {
			<-start
			_, found, _, err := store.ExtendLongCooldown(context.Background(), channel, key, addEach)
			if err != nil {
				errCount.add(1)
				return
			}
			if found {
				foundCount.add(1)
			}
		})
	}
	close(start)
	group.Wait()

	require.True(t, claimWon, "the only claimer always wins an unheld window")
	require.Zero(t, errCount.load(), "no extend ever errored, whatever order it ran in")

	windows := r.storedWindows(t)
	require.Len(t, windows, 1, "still exactly one row for this window")
	base := claimed.Windows[0].ExpiresAtMS
	assert.Equal(t, base+foundCount.load()*addEach*1000, windows[0],
		"every extend that reported found added exactly once; extends before the claim existed reported not_running and cost nothing")
}

type atomic64 struct {
	mu sync.Mutex
	n  int64
}

func (a *atomic64) add(d int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n += d
}

func (a *atomic64) load() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

// A release names the expiry it claimed, not just the key: after an extend moves that expiry forward, a release
// built from the stale, pre-extend hold matches no row any more and gives nothing back. The extended time survives.
func TestReleaseAfterAnExtendLeavesTheExtendedWindowStanding(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_release_after_extend_"), Dialect: "mysql"})
	channel := uniqueChannel(49)
	r.channel = channel
	key := automation.LongCooldownNamedKey(channel, namedCooldownName)
	store := r.longStore()
	ctx := context.Background()

	hold, won, err := store.ClaimLongCooldowns(ctx, engine.LongCooldownClaim{BroadcasterID: channel, Windows: []engine.CooldownWindow{{Key: key, TTL: time.Hour}}})
	require.NoError(t, err)
	require.True(t, won)
	original := hold.Windows[0].ExpiresAtMS

	extended, found, capped, err := store.ExtendLongCooldown(ctx, channel, key, 600)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, capped)
	require.Greater(t, extended, original)

	require.NoError(t, store.ReleaseLongCooldowns(ctx, hold))

	windows := r.storedWindows(t)
	require.Len(t, windows, 1, "the stale release matched nothing, so the row is untouched")
	assert.Equal(t, extended, windows[0], "the extended expiry survives a release built from the pre-extend hold")
}

// A failing step does not undo the ones around it: mixed steps are not all-or-nothing, only the window claim is.
func TestMixedStepsAreNotAllOrNothingOnlyTheWindowClaimIs(t *testing.T) {
	r := startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_mixed_steps_"), Dialect: "mysql"})
	r.channel = uniqueChannel(50)
	raw, err := codec.Marshal(step{
		"version":    1,
		"trigger":    step{"type": "command", "params": step{"name": "!mixed"}},
		"conditions": []step{{"type": "perm", "params": step{"level": "mod"}}},
		"actions": []step{
			chatStep("before"),
			{"type": "cooldown_add", "params": step{"name": "unowned", "seconds": 10}},
			{"type": "math", "params": step{"expression": "1/0", "save_as": "x"}},
			chatStep("after"),
		},
	})
	require.NoError(t, err)
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(raw, &def))
	require.Empty(t, automation.Validate(def))
	r.source.set(r.channel, automation.Automation{ID: "mixed", UserID: r.channel, Name: "mixed", Enabled: true, Definition: def})

	r.runAutomationCommandAsMod(t, r.nextEvent("mixed"), alice, "!mixed")
	rec := r.awaitAutomationRun(t, "mixed", 1)

	require.Len(t, rec.Actions, 4, statusesOf(rec))
	assert.Equal(t, automationrpc.StatusOK, rec.Actions[0].Status, "the chat step before the failure ran")
	assert.Equal(t, "not_running", rec.Actions[1].Detail, "the unowned named window was never armed")
	assert.Equal(t, automationrpc.StatusFailed, rec.Actions[2].Status, "the math step failed outright")
	assert.Equal(t, automationrpc.StatusOK, rec.Actions[3].Status, "the chat step after the failure still ran")
	assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome, "one real failure among otherwise-OK steps reads as partial, not rolled back")
	assert.Equal(t, []string{"before", "after"}, r.channelChat())
}

// runAutomationCommandAsMod sends a chat command from a viewer carrying the moderator badge, so a step that
// requires it (cooldown_add included) is reachable under a command trigger.
func (r *cooldownRig) runAutomationCommandAsMod(t *testing.T, eventID string, viewer uint64, text string) {
	t.Helper()
	id := strconv.FormatUint(viewer, 10)
	r.process(t, 0, lane.Envelope{
		Type: "channel.chat.message", Lane: "premium", EventID: eventID, Text: text,
		BroadcasterUserID: strconv.FormatUint(r.channel, 10), BroadcasterUserLogin: "streamer",
		ChatterUserID: id, ChatterUserLogin: "viewer" + id,
		Badges: []lane.Badge{{SetID: "moderator"}},
	})
}
