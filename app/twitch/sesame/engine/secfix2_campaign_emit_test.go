// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestCapChatTextPassthrough(t *testing.T) {
	for _, text := range []string{
		"",
		"hello",
		"a\nb\nc\nd\ne",
		strings.Repeat("x", 500),
	} {
		got, changed := capChatText(text)
		assert.False(t, changed)
		assert.Equal(t, text, got)
	}
}

func TestCapChatTextTruncates(t *testing.T) {
	got, changed := capChatText("1\n2\n3\n4\n5\n6")
	assert.True(t, changed)
	assert.Equal(t, "1\n2\n3\n4\n5", got)

	got, changed = capChatText(strings.Repeat("a", 501))
	assert.True(t, changed)
	assert.Len(t, got, 500)
}

func TestCapChatTextRuneSafe(t *testing.T) {
	confetti := "\U0001F389"
	line := strings.Repeat("a", 498) + confetti + confetti
	got, changed := capChatText(line)
	assert.True(t, changed)
	assert.LessOrEqual(t, len(got), 500)
	require.NotPanics(t, func() { _, _ = codec.Marshal(got) })
}

func TestEmitCapsOversizedChatOutput(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{},
		emitModule("", module.KindCore, strings.Repeat("a", 600)+"\n"+strings.Repeat("b", 600)+"\nthird"))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hello")))
	require.Len(t, pub.got, 1)

	var body struct {
		Message string `json:"message"`
	}
	require.NoError(t, codec.Unmarshal(pub.got[0].msg.Payload, &body))
	assert.Equal(t,
		strings.Repeat("a", 500)+"\n"+strings.Repeat("b", 500)+"\nthird",
		body.Message,
		"each oversized line is capped in place; nothing over five lines goes out")
}

func TestExternalVarCapsAndSanitizes(t *testing.T) {
	assert.Equal(t, "sniper", ExternalVar("sniper"))
	assert.Equal(t, "evil/ban everyone bot", ExternalVar("evil\n/ban everyone bot"))
	assert.Equal(t, "ban everyone", ExternalVar("/ban everyone"))
	long := strings.Repeat("x", 300) + "\U0001F389"
	got := ExternalVar(long)
	assert.LessOrEqual(t, len(got), MaxExternalVarBytes)
	assert.Equal(t, strings.Repeat("x", MaxExternalVarBytes), got)
}

type recordingReputation struct {
	scores map[string]int
	bumps  []string
}

func newRecordingReputation() *recordingReputation {
	return &recordingReputation{scores: map[string]int{}}
}

func (r *recordingReputation) Score(_ context.Context, id string) int { return r.scores[id] }

func (r *recordingReputation) Bump(_ context.Context, id string) { r.bumps = append(r.bumps, id) }

func campaignPipeline(pub *fakePublisher, camp Campaign, rep Reputation, zc zapcore.Core) *Pipeline {
	gate := automod.New()
	gate.SetEmotes(automod.NewEmoteSet(nil))
	log := zap.NewNop()
	if zc != nil {
		log = zap.New(zc)
	}
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: log, Automod: gate, Campaign: camp, Reputation: rep,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: true,
	})
}

func TestCampaignOnlyVerdictSkipsReputationStrikeButAudits(t *testing.T) {
	pub := &fakePublisher{}
	rep := newRecordingReputation()
	core, logs := observer.New(zapcore.DebugLevel)
	p := campaignPipeline(pub, &fakeCampaign{count: campaignThreshold}, rep, core)

	require.NoError(t, p.Process(linkChat(t, "777")))
	require.Len(t, pub.got, 1, "the delete still fires")
	assert.Equal(t, outgress.TypeDelete, pub.got[0].msg.Type)
	assert.Empty(t, rep.bumps, "an attacker-minted quorum must not score a strike")

	matched := logs.FilterMessage("campaign band quorum")
	require.Equal(t, 1, matched.Len(), "one audit line per quorum activation")
	assert.Equal(t, "777", matched.All()[0].ContextMap()["chatter_id"])
}

func TestContentBackedCampaignEscalationStillScores(t *testing.T) {
	pub := &fakePublisher{}
	rep := newRecordingReputation()
	p := campaignPipeline(pub, &fakeCampaign{count: campaignThreshold}, rep, nil)

	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "888",
		"msg_id":              "m-888",
		"text":                "FREE VBUCKS CLICK MY PROFILE RIGHT NOW EVERYONE HURRY bit.ly/free-vbucks",
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("u-888", body)))

	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type, "delete + campaign quorum = timeout")
	assert.Equal(t, []string{"888"}, rep.bumps, "content-backed verdict scores normally")
}
