// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	leftCommand   = "!left"
	threeDays     = 3 * 86_400
	rewardUUID    = "11111111-2222-3333-4444-555555555555"
	readyDefault  = ", this is ready to use."
	waitingSuffix = " left before you can use this again.$"
	// The time left rounds up to a whole second, so a fresh window may still read as its full length.
	twoDaysLeft   = "(2d|1d 23h)"
	threeDaysLeft = "(3d|2d 23h)"
	oneHourLeft   = "(1h|59m .*s)"
)

type chatSent struct {
	channel string
	text    string
}

func (r *cooldownRig) recordChat(msg *nats.Msg) {
	var sent outgress.Message
	if codec.Unmarshal(msg.Data, &sent) != nil {
		return
	}
	items := []outgress.Message{sent}
	var batch outgress.Batch
	if sent.Type == outgress.TypeBatch && codec.Unmarshal(sent.Payload, &batch) == nil {
		items = batch.Items
	}
	r.chatMu.Lock()
	defer r.chatMu.Unlock()
	for _, item := range items {
		var body struct {
			Message string `json:"message"`
		}
		if item.Type == outgress.TypeChat && codec.Unmarshal(item.Payload, &body) == nil {
			r.chat = append(r.chat, chatSent{channel: item.BroadcasterID, text: body.Message})
		}
	}
}

func (r *cooldownRig) channelChat() []string {
	id := strconv.FormatUint(r.channel, 10)
	r.chatMu.Lock()
	defer r.chatMu.Unlock()
	var lines []string
	for _, sent := range r.chat {
		if sent.channel == id {
			lines = append(lines, sent.text)
		}
	}
	return lines
}

func (r *cooldownRig) say(t *testing.T, viewer uint64, text string) {
	t.Helper()
	id := strconv.FormatUint(viewer, 10)
	r.process(t, 0, lane.Envelope{
		Type: "channel.chat.message", Lane: "premium", EventID: r.nextEvent("left"), Text: text,
		BroadcasterUserID: strconv.FormatUint(r.channel, 10), BroadcasterUserLogin: "streamer",
		ChatterUserID: id, ChatterUserLogin: "viewer" + id, ChatterUserName: "Viewer" + id,
	})
}

// peek asks the time left command and returns the single reply it sent.
func (r *cooldownRig) peek(t *testing.T, viewer uint64) string {
	t.Helper()
	before := len(r.channelChat())
	r.say(t, viewer, leftCommand)
	var lines []string
	require.Eventually(t, func() bool {
		lines = r.channelChat()
		return len(lines) > before
	}, settle, 10*time.Millisecond)
	return lines[len(lines)-1]
}

func (r *cooldownRig) assertSilent(t *testing.T, viewer uint64) {
	t.Helper()
	before := len(r.channelChat())
	r.say(t, viewer, leftCommand)
	require.NoError(t, r.nc.Flush())
	time.Sleep(100 * time.Millisecond)
	assert.Len(t, r.channelChat(), before)
}

func (r *cooldownRig) peekCount(t *testing.T) int64 {
	t.Helper()
	require.NoError(t, r.nc.Flush())
	time.Sleep(50 * time.Millisecond)
	return r.peekCalls.Load()
}

func definitionOf(t *testing.T, id string, channel uint64, raw step) automation.Automation {
	t.Helper()
	raw["version"] = 1
	body, err := codec.Marshal(raw)
	require.NoError(t, err)
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(body, &def))
	require.Empty(t, automation.Validate(def))
	return automation.Automation{ID: id, UserID: channel, Name: id, Enabled: true, Definition: def}
}

func (r *cooldownRig) timeLeftRecipe(t *testing.T, channel uint64, cooldown step) {
	t.Helper()
	r.channel, r.runsNow = channel, 0
	cooldown["left_command"] = leftCommand
	r.source.set(channel, definitionOf(t, cooldownRecipe, channel, step{
		"trigger":    step{"type": "command", "params": step{"name": cooldownRecipe}},
		"conditions": []step{{"type": "cooldown", "params": cooldown}},
		"actions":    []step{chatStep("ran")},
	}))
}

func (r *cooldownRig) ifRecipe(t *testing.T, channel uint64, otherwise []step, then ...step) {
	t.Helper()
	r.channel, r.runsNow = channel, 0
	branch := step{"type": "if", "params": step{
		"condition": step{"type": "cooldown", "params": step{"global_s": twoDaysSeconds}},
		"then":      then,
		"else":      otherwise,
	}}
	r.source.set(channel, definitionOf(t, cooldownRecipe, channel, step{
		"trigger": step{"type": "command", "params": step{"name": cooldownRecipe}},
		"actions": []step{branch, chatStep("after [{cooldown:left}]")},
	}))
}

func (r *cooldownRig) awaitChatTail(t *testing.T, count int) []string {
	t.Helper()
	var lines []string
	require.Eventually(t, func() bool {
		lines = r.channelChat()
		return len(lines) >= count
	}, settle, 10*time.Millisecond)
	return lines[len(lines)-count:]
}

func exerciseTimeLeft(t *testing.T, r *cooldownRig) {
	t.Run("a peek shows the time left and spends nothing", func(t *testing.T) { peekSpendsNothing(t, r) })
	t.Run("a peek when ready says so and leaves the window free", func(t *testing.T) { peekWhenReady(t, r) })
	t.Run("the longer of viewer and global wins", func(t *testing.T) { peekLongestWindow(t, r) })
	t.Run("a short window reads its Valkey key", func(t *testing.T) { peekShortWindow(t, r) })
	t.Run("a moved window answers from Valkey", func(t *testing.T) { peekAfterMove(t, r) })
	t.Run("a marker miss asks once and marks again", func(t *testing.T) { peekMarkerMiss(t, r) })
	t.Run("a reward recipe answers its command", func(t *testing.T) { peekRewardRecipe(t, r) })
	t.Run("a disabled recipe is silent", func(t *testing.T) { peekDisabledRecipe(t, r) })
	t.Run("custom replies render", func(t *testing.T) { peekCustomReplies(t, r) })
	t.Run("the failed branch shows the time left", func(t *testing.T) { leftInFailedBranch(t, r) })
	t.Run("a stored value holding the token stays literal", func(t *testing.T) { leftStaysLiteralInStoredValue(t, r) })
}

func peekSpendsNothing(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(41), step{"global_s": twoDaysSeconds})
	r.assertPasses(t, alice)
	stored := r.storedWindows(t)
	claims, peeks := r.claimCount(t), r.peekCount(t)
	ttl := r.markerTTL(t, r.globalKey())
	runs := r.runsNow

	first := r.peek(t, bob)
	second := r.peek(t, bob)

	assert.Regexp(t, "^Viewer102, "+twoDaysLeft+waitingSuffix, first)
	assert.Regexp(t, "^Viewer102, "+twoDaysLeft+waitingSuffix, second)
	assert.Equal(t, stored, r.storedWindows(t), "the row did not move")
	assert.InDelta(t, float64(ttl), float64(r.markerTTL(t, r.globalKey())), float64(5*time.Second), "the marker did not move")
	assert.EqualValues(t, claims, r.claimCount(t), "no claim RPC")
	assert.EqualValues(t, peeks, r.peekCount(t), "no peek RPC while the marker lives")
	assert.Equal(t, runs, r.runsNow, "no run started")
	r.assertRefused(t, bob)
}

func peekWhenReady(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(42), step{"global_s": twoDaysSeconds, "user_s": threeDays})

	assert.Equal(t, "Viewer101"+readyDefault, r.peek(t, alice))
	assert.Empty(t, r.storedWindows(t))

	r.assertPasses(t, alice)
}

func peekLongestWindow(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(43), step{"global_s": twoDaysSeconds, "user_s": threeDays})
	r.assertPasses(t, alice)

	assert.Regexp(t, "^Viewer101, "+threeDaysLeft+waitingSuffix, r.peek(t, alice), "the viewer window runs longer")
	assert.Regexp(t, "^Viewer102, "+twoDaysLeft+waitingSuffix, r.peek(t, bob), "another viewer only has the global window")
}

func peekShortWindow(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(44), step{"global_s": oneHourSeconds})
	r.assertPasses(t, alice)
	peeks := r.peekCount(t)

	assert.Regexp(t, "^Viewer102, "+oneHourLeft+waitingSuffix, r.peek(t, bob))
	assert.EqualValues(t, peeks, r.peekCount(t), "a short window never asks modules")
}

func peekAfterMove(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(45), step{"global_s": twoDaysSeconds})
	r.assertPasses(t, alice)
	r.dropMarker(t, r.globalKey())
	r.setExpiry(t, r.globalKey(), time.Now().Add(time.Hour))
	require.NoError(t, r.mover("solo").MoveDue(context.Background()))
	require.Empty(t, r.storedWindows(t))
	peeks := r.peekCount(t)

	assert.Regexp(t, "^Viewer102, "+oneHourLeft+waitingSuffix, r.peek(t, bob))
	assert.EqualValues(t, peeks, r.peekCount(t))
}

func peekMarkerMiss(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(46), step{"global_s": twoDaysSeconds})
	r.assertPasses(t, alice)
	r.dropMarker(t, r.globalKey())
	peeks := r.peekCount(t)

	assert.Regexp(t, "^Viewer102, "+twoDaysLeft+waitingSuffix, r.peek(t, bob))
	assert.EqualValues(t, 1, r.peekCount(t)-peeks, "one read of the row")
	assert.Positive(t, r.markerTTL(t, r.globalKey()), "the read marked again")

	r.peek(t, bob)
	assert.EqualValues(t, 1, r.peekCount(t)-peeks, "the new marker answers the next peek")
}

func peekRewardRecipe(t *testing.T, r *cooldownRig) {
	channel := uniqueChannel(47)
	r.channel, r.runsNow = channel, 0
	r.source.set(channel, definitionOf(t, "reward-recipe", channel, step{
		"trigger":    step{"type": "reward", "params": step{"reward_id": rewardUUID}},
		"conditions": []step{{"type": "cooldown", "params": step{"user_s": twoDaysSeconds, "left_command": leftCommand}}},
		"actions":    []step{chatStep("redeemed")},
	}))
	id := strconv.FormatUint(alice, 10)
	event := `{"id":"red-1","user_id":"` + id + `","user_login":"viewer` + id + `","user_name":"Viewer` + id + `","reward":{"id":"` + rewardUUID + `","title":"Hydrate"}}`

	assert.Equal(t, "Viewer101"+readyDefault, r.peek(t, alice))
	r.process(t, 0, lane.Envelope{
		Type: automation.RewardRedemptionKind, Lane: "premium", EventID: r.nextEvent("redeem"),
		BroadcasterUserID: strconv.FormatUint(channel, 10), BroadcasterUserLogin: "streamer", Event: codec.RawMessage(event),
	})
	assert.Equal(t, []string{"redeemed"}, r.awaitChatTail(t, 1))

	assert.Regexp(t, "^Viewer101, "+twoDaysLeft+waitingSuffix, r.peek(t, alice))
	assert.Equal(t, "Viewer102"+readyDefault, r.peek(t, bob), "the window belongs to the redeeming viewer")
}

func peekDisabledRecipe(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(48), step{"global_s": twoDaysSeconds})
	r.assertPasses(t, alice)
	recipes, err := r.source.Automations(context.Background(), r.channel)
	require.NoError(t, err)
	recipes[0].Enabled = false
	r.source.set(r.channel, recipes[0])

	r.assertSilent(t, bob)
}

func peekCustomReplies(t *testing.T, r *cooldownRig) {
	r.timeLeftRecipe(t, uniqueChannel(49), step{"global_s": twoDaysSeconds, "left_reply": "{cooldown:left} more, {user}", "ready_reply": "go {user} [{cooldown:left}]"})

	assert.Equal(t, "go Viewer101 []", r.peek(t, alice))
	r.assertPasses(t, alice)
	assert.Regexp(t, "^"+twoDaysLeft+" more, Viewer102$", r.peek(t, bob))
}

func leftInFailedBranch(t *testing.T, r *cooldownRig) {
	r.ifRecipe(t, uniqueChannel(50), []step{chatStep("wait {cooldown:left}")}, chatStep("won [{cooldown:left}]"))

	r.assertPasses(t, alice)
	assert.Equal(t, []string{"won []", "after []"}, r.awaitChatTail(t, 2), "empty outside a refusal")

	r.assertPasses(t, bob)
	refused := r.awaitChatTail(t, 2)
	assert.Regexp(t, "^wait "+twoDaysLeft+"$", refused[0], "the refused branch")
	assert.Regexp(t, `^after \[`+twoDaysLeft+`\]$`, refused[1], "and the steps after it")
}

func leftStaysLiteralInStoredValue(t *testing.T, r *cooldownRig) {
	r.ifRecipe(t, uniqueChannel(51), []step{chatStep("saved {var:raw}")}, chatStep("won"))
	owner := engine.VariableOwner{BroadcasterID: r.channel, Scope: automation.VariableScopeChannel}
	require.NoError(t, r.vars.VariableSet(context.Background(), owner, "raw", "{cooldown:left}"))
	r.assertPasses(t, alice)
	r.awaitChatTail(t, 2)

	r.assertPasses(t, bob)

	assert.Equal(t, "saved {cooldown:left}", r.awaitChatTail(t, 2)[0])
}

func TestTimeLeftThroughNATSAndModulesOnSQLite(t *testing.T) {
	exerciseTimeLeft(t, startCooldownRig(t, sqliteRecipeDB(t)))
}

func TestTimeLeftThroughNATSAndModulesOnMySQL(t *testing.T) {
	exerciseTimeLeft(t, startCooldownRig(t, recipeDB{Pool: testdb.MySQL(t, "bagel_cooldown_left_"), Dialect: "mysql"}))
}
