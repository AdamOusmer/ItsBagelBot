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

// --- emit-side chat cap (the runtime backstop for pre-walk stored configs) ---

// Untouched output is returned byte-identical: the common case must not pay a
// rebuild (or drift by a join/split round trip).
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
	// Six lines -> first five.
	got, changed := capChatText("1\n2\n3\n4\n5\n6")
	assert.True(t, changed)
	assert.Equal(t, "1\n2\n3\n4\n5", got)

	// Oversized line cut to the byte limit.
	got, changed = capChatText(strings.Repeat("a", 501))
	assert.True(t, changed)
	assert.Len(t, got, 500)
}

// A byte-exact cut could split a multibyte rune and hand outgress an invalid
// UTF-8 sequence; the cut backs up to a rune start instead.
func TestCapChatTextRuneSafe(t *testing.T) {
	confetti := "\U0001F389" // 4 bytes
	line := strings.Repeat("a", 498) + confetti + confetti
	got, changed := capChatText(line)
	assert.True(t, changed)
	assert.LessOrEqual(t, len(got), 500)
	require.NotPanics(t, func() { _, _ = codec.Marshal(got) })
}

// The full emit path: a module (or old stored config) that renders an oversized
// line publishes a bounded message instead of a guaranteed Twitch 400.
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

// --- external variable capping ---

func TestExternalVarCapsAndSanitizes(t *testing.T) {
	assert.Equal(t, "sniper", ExternalVar("sniper"))
	// Control characters stripped, so no embedded newline can mint extra
	// lines (the leftover "/" is inert mid-line; only a LEADING slash run is
	// a verb vector and sanitizeVar trims that too).
	assert.Equal(t, "evil/ban everyone bot", ExternalVar("evil\n/ban everyone bot"))
	assert.Equal(t, "ban everyone", ExternalVar("/ban everyone"))
	// ...and length capped at MaxExternalVarBytes without splitting runes.
	long := strings.Repeat("x", 300) + "\U0001F389"
	got := ExternalVar(long)
	assert.LessOrEqual(t, len(got), MaxExternalVarBytes)
	assert.Equal(t, strings.Repeat("x", MaxExternalVarBytes), got)
}

// --- campaign-band poisoning mitigations ---

// recordingReputation records strikes so the attribution rule can be observed:
// council-only verdicts must not score, content-backed ones must.
type recordingReputation struct {
	scores map[string]int
	bumps  []string
}

func newRecordingReputation() *recordingReputation {
	return &recordingReputation{scores: map[string]int{}}
}

func (r *recordingReputation) Score(_ context.Context, id string) int { return r.scores[id] }

func (r *recordingReputation) Bump(_ context.Context, id string) { r.bumps = append(r.bumps, id) }

// campaignPipeline is councilPipeline with a reputation store and an injectable
// zap core (pass nil for a nop logger).
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

// A band quorum with NO content signal behind it (clean link carrier, delete
// minted purely by "council:campaign") enforces the immediate delete — a real
// flood needs it — but records NO reputation strike: 8 colluding accounts can
// manufacture the quorum, so letting it score would let them pump an innocent
// user up the warn->timeout->ban ladder while the reason launders it as
// automod. The activation itself lands one Warn audit line naming the channel
// and the actioned chatter.
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

// A content-backed verdict escalated BY the campaign juror keeps its strike:
// the content signal alone justifies it regardless of what the band said.
// (The link makes the base evidence content-backed — under the jury rule a
// style-only delete at quorum stays delete and is covered over in
// council_test.go.)
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
