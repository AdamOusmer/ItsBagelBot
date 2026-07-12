package engine

import (
	"strconv"
	"testing"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// hostileCohort builds a folded channel.chat.message: n distinct senders, all
// carrying the same text (which the gate flags as a timeout when hostile).
func hostileCohort(t *testing.T, n int, text string) *message.Message {
	t.Helper()
	senders := make([]map[string]any, n)
	for i := range senders {
		senders[i] = map[string]any{"chatter_user_id": strconv.Itoa(i + 1)}
	}
	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"text":                text,
		"senders":             senders,
	})
	require.NoError(t, err)
	return message.NewMessage("cohort", body)
}

func raidPipeline(t *testing.T, pub bus.Publisher, enforce, shield bool) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(),
	}
	cfg := Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj,
		AutomodEnforce: enforce, ShieldEnabled: shield,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), cfg)
}

// countByType tallies the captured outgress messages by their Type.
func countByType(got []captured) map[string]int {
	m := map[string]int{}
	for _, c := range got {
		m[c.msg.Type]++
	}
	return m
}

const raidLink = "everyone hurry claim your prize at grabify.link/xyz right now"

func TestMassRaidEscalatesToShieldAndBansCappedPrefix(t *testing.T) {
	pub := &fakePublisher{}
	p := raidPipeline(t, pub, true, true)

	// A cohort larger than both the raid threshold and the ban cap.
	require.NoError(t, p.Process(hostileCohort(t, massRaidBanCap+30, raidLink)))

	got := countByType(pub.got)
	assert.Equal(t, 1, got[outgress.TypeShieldMode], "one channel-level Shield Mode activation")
	assert.Equal(t, massRaidBanCap, got[outgress.TypeTimeout], "per-account bans are capped")
	assert.Len(t, pub.got, 1+massRaidBanCap, "shield + capped bans, nothing else")
}

func TestSmallHostileCohortBansAllNoShield(t *testing.T) {
	pub := &fakePublisher{}
	p := raidPipeline(t, pub, true, true)

	n := massRaidThreshold - 1 // below the raid threshold
	require.NoError(t, p.Process(hostileCohort(t, n, raidLink)))

	got := countByType(pub.got)
	assert.Zero(t, got[outgress.TypeShieldMode], "a small cohort never trips Shield Mode")
	assert.Equal(t, n, got[outgress.TypeTimeout], "every member of a small hostile cohort is actioned")
}

func TestMassRaidShieldDisabledStillBans(t *testing.T) {
	pub := &fakePublisher{}
	p := raidPipeline(t, pub, true, false) // enforce on, shield off

	require.NoError(t, p.Process(hostileCohort(t, massRaidThreshold+5, raidLink)))

	got := countByType(pub.got)
	assert.Zero(t, got[outgress.TypeShieldMode], "shield off means no Shield Mode")
	assert.Equal(t, massRaidThreshold+5, got[outgress.TypeTimeout], "the cohort is still banned within the cap")
}

func TestCleanCohortDoesNotAct(t *testing.T) {
	pub := &fakePublisher{}
	p := raidPipeline(t, pub, true, true)

	// Hype copypasta: many identical senders but harmless text. The gate returns
	// no verdict, so nothing is emitted (only reputation would build).
	require.NoError(t, p.Process(hostileCohort(t, massRaidThreshold+10, "PogChamp what a play")))
	assert.Empty(t, pub.got, "a clean cohort is never actioned")
}

func TestMassRaidShadowModeDoesNotAct(t *testing.T) {
	pub := &fakePublisher{}
	p := raidPipeline(t, pub, false, true) // shadow: enforce off

	require.NoError(t, p.Process(hostileCohort(t, massRaidThreshold+10, raidLink)))
	assert.Empty(t, pub.got, "shadow mode emits nothing, even for a mass raid")
}

func TestRepeatedFoldsShieldOncePerCooldown(t *testing.T) {
	pub := &fakePublisher{}
	p := raidPipeline(t, pub, true, true)

	// Two folds from the same raid on the same channel within the cooldown.
	require.NoError(t, p.Process(hostileCohort(t, massRaidThreshold+1, raidLink)))
	require.NoError(t, p.Process(hostileCohort(t, massRaidThreshold+1, raidLink)))

	assert.Equal(t, 1, countByType(pub.got)[outgress.TypeShieldMode],
		"a second fold within the cooldown must not re-activate Shield Mode")
}

func TestIsMassRaid(t *testing.T) {
	timeout := automod.Verdict{Action: automod.ActionTimeout}
	ban := automod.Verdict{Action: automod.ActionBan}
	del := automod.Verdict{Action: automod.ActionDelete}

	assert.True(t, isMassRaid(timeout, massRaidThreshold))
	assert.True(t, isMassRaid(ban, massRaidThreshold))
	assert.False(t, isMassRaid(timeout, massRaidThreshold-1), "below threshold is not a mass raid")
	assert.False(t, isMassRaid(del, massRaidThreshold), "a delete/heuristic verdict is never a raid")
}

func TestBuildOutgressShieldMode(t *testing.T) {
	body, err := buildOutgress(&module.Output{
		Type:          outgress.TypeShieldMode,
		BroadcasterID: "77",
	})
	require.NoError(t, err)

	var msg outgress.Message
	require.NoError(t, sonic.Unmarshal(body, &msg))
	assert.Equal(t, outgress.TypeShieldMode, msg.Type)
	assert.Equal(t, "77", msg.BroadcasterID)
	assert.JSONEq(t, `{"is_active":true}`, string(msg.Payload))
}

// A cohort must never dispatch a command even when Automod is present and the
// broadcaster has a matching custom command; the enforced-cohort path replaces
// dispatch entirely.
func TestHostileCohortWithCommandStillNoDispatch(t *testing.T) {
	reader := fakeReader{cmd: projection.Command{Name: "x", Response: "hi", IsActive: true}, cmdFound: true}
	pub := &fakePublisher{}
	d := Deps{Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop(), Automod: automod.New()}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true, ShieldEnabled: true})

	require.NoError(t, p.Process(hostileCohort(t, 3, raidLink)))
	for _, c := range pub.got {
		assert.NotEqual(t, outgress.TypeChat, c.msg.Type, "a cohort must never emit a chat reply")
	}
}
