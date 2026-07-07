package engine

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeCampaign returns a fixed distinct-sender count and records how often it
// was consulted.
type fakeCampaign struct {
	mu    sync.Mutex
	count int
	calls int
}

func (c *fakeCampaign) Observe(context.Context, uint64, string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.count
}

func councilPipeline(pub *fakePublisher, camp Campaign) *Pipeline {
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(), Campaign: camp,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true,
	})
}

// linkChat is a clean, link-bearing chat line (no content verdict on its own)
// with a message id so deletes can be exercised.
func linkChat(t *testing.T, chatter string) *message.Message {
	t.Helper()
	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     chatter,
		"msg_id":              "m-" + chatter,
		"text":                "hey friends come check this great video at https://example.com/watch tonight",
	})
	require.NoError(t, err)
	return message.NewMessage("u-"+chatter, body)
}

func TestCampaignEscalatesLinkFloodToDelete(t *testing.T) {
	camp := &fakeCampaign{count: campaignThreshold}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	require.NoError(t, p.Process(linkChat(t, "777")))
	require.Len(t, pub.got, 1, "campaign corroboration adds the mildest action")
	assert.Equal(t, outgress.TypeDelete, pub.got[0].msg.Type)
	assert.Equal(t, "m-777", pub.got[0].msg.MsgID)
	assert.Equal(t, 1, camp.calls)
}

func TestCampaignBelowThresholdDoesNothing(t *testing.T) {
	camp := &fakeCampaign{count: campaignThreshold - 1}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	require.NoError(t, p.Process(linkChat(t, "777")))
	assert.Empty(t, pub.got, "below the quorum the campaign juror abstains")
	assert.Equal(t, 1, camp.calls, "the line was still counted")
}

func TestCampaignNotConsultedOnCleanShortChat(t *testing.T) {
	camp := &fakeCampaign{count: 100}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	require.NoError(t, p.Process(chatMsg(t, "standard", "nice play")))
	assert.Empty(t, pub.got)
	assert.Zero(t, camp.calls, "a clean short line never reaches the campaign juror")
}

func TestCampaignEscalatesFlaggedDeleteToTimeout(t *testing.T) {
	camp := &fakeCampaign{count: campaignThreshold}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	// A caps heuristic line (delete verdict) corroborated by the campaign juror
	// becomes a timeout.
	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "888",
		"msg_id":              "m-888",
		"text":                "FREE VBUCKS CLICK MY PROFILE RIGHT NOW EVERYONE HURRY",
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(message.NewMessage("u", body)))

	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type, "delete + campaign quorum = timeout")
}

func TestHarassmentWarnPairsWithDelete(t *testing.T) {
	pub := &fakePublisher{}
	p := councilPipeline(pub, nil)

	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"msg_id":              "m-999",
		"text":                "nobody asked just go kill yourself already dude seriously",
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(message.NewMessage("u", body)))

	got := countByType(pub.got)
	assert.Equal(t, 1, got[outgress.TypeWarn], "harassment issues a formal warning")
	assert.Equal(t, 1, got[outgress.TypeDelete], "and removes the message")
}

func TestWarnLadderEscalatesByReputation(t *testing.T) {
	warn := automod.Verdict{Action: automod.ActionWarn, Rule: "lex:harassment:x"}
	v := escalateByReputation(warn, repWarnToTimeoutScore)
	assert.Equal(t, automod.ActionTimeout, v.Action, "a repeat offender's warn becomes a timeout")
	assert.EqualValues(t, 600, v.Seconds)

	v = escalateByReputation(warn, 0)
	assert.Equal(t, automod.ActionWarn, v.Action, "first strike stays a warn")

	timeout := automod.Verdict{Action: automod.ActionTimeout, Seconds: 600}
	assert.Equal(t, automod.ActionBan, escalateByReputation(timeout, repEscalateThreshold).Action)
}

func TestBuildOutgressDeleteAndWarn(t *testing.T) {
	body, err := buildOutgress(&module.Output{
		Type:          outgress.TypeDelete,
		BroadcasterID: "77",
		MsgID:         "abc-123",
	})
	require.NoError(t, err)
	var msg outgress.Message
	require.NoError(t, sonic.Unmarshal(body, &msg))
	assert.Equal(t, outgress.TypeDelete, msg.Type)
	assert.Equal(t, "abc-123", msg.MsgID)
	// A nil RawMessage marshals as JSON null: no body either way.
	assert.True(t, len(msg.Payload) == 0 || string(msg.Payload) == "null", "delete has no body")

	body, err = buildOutgress(&module.Output{
		Type:          outgress.TypeWarn,
		BroadcasterID: "77",
		TargetUserID:  "999",
		Reason:        "automod:lex:harassment:x",
	})
	require.NoError(t, err)
	require.NoError(t, sonic.Unmarshal(body, &msg))
	assert.Equal(t, outgress.TypeWarn, msg.Type)
	var wire banBodyWire
	require.NoError(t, sonic.Unmarshal(msg.Payload, &wire))
	assert.Equal(t, "999", wire.Data.UserID)
	assert.Equal(t, "automod:lex:harassment:x", wire.Data.Reason)
}

// The campaign juror must never fire on ordinary chat volume: distinct chatters
// posting DIFFERENT clean lines share no template, and the fake here proves the
// pipeline only consults the juror for link-bearing or already-flagged lines.
func TestOrdinaryChatterFlowNeverCounts(t *testing.T) {
	camp := &fakeCampaign{count: 100}
	pub := &fakePublisher{}
	p := councilPipeline(pub, camp)

	for i := 0; i < 5; i++ {
		require.NoError(t, p.Process(chatMsg(t, "standard", "that jungle gank was so clean honestly round "+strconv.Itoa(i))))
	}
	assert.Zero(t, camp.calls)
	assert.Empty(t, pub.got)
}
