// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// hypeChat is a caps-only line whose every token is covered by a native emote
// span: exactly the shape the learned layers exist to rescue, and exactly the
// FP class the 2026-08-22 shadow audit caught (0/8 precision, "LUL LUL LUL LUL").
func hypeChat(t *testing.T) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"msg_id":              "m-999",
		"text":                "LUL LUL LUL LUL",
		"emotes": []map[string]any{
			{"id": "425618", "begin": 0, "end": 3},
			{"id": "425618", "begin": 4, "end": 7},
			{"id": "425618", "begin": 8, "end": 11},
			{"id": "425618", "begin": 12, "end": 15},
		},
	})
	require.NoError(t, err)
	return bus.NewMessage("u-999", body)
}

// darkLaunchPipeline mirrors councilPipeline with the adaptive switch exposed:
// the fetched emote set is loaded-but-empty so caps keeps enforcing, which
// makes per-message spans the only possible rescue for the hype line.
func darkLaunchPipeline(pub *fakePublisher, adaptive bool) *Pipeline {
	gate := automod.New()
	gate.SetEmotes(automod.NewEmoteSet(nil))
	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: gate,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj,
		AutomodEnforce: true, AdaptiveEnabled: adaptive,
	})
}

// Dark launch contract: SESAME_AUTOMOD_ADAPTIVE unset must leave verdicts
// byte-identical to the pre-span gate — the envelope may carry emote data,
// but the pipeline drops it at the door.
func TestAdaptiveOffDropsSpanKnowledge(t *testing.T) {
	pub := &fakePublisher{}
	p := darkLaunchPipeline(pub, false)

	require.NoError(t, p.Process(hypeChat(t)))
	require.Len(t, pub.got, 1, "pre-learned gate still deletes caps-only hype")
	assert.Equal(t, outgress.TypeDelete, pub.got[0].msg.Type)
}

func TestAdaptiveOnRescuesSpanCoveredHype(t *testing.T) {
	pub := &fakePublisher{}
	p := darkLaunchPipeline(pub, true)

	require.NoError(t, p.Process(hypeChat(t)))
	assert.Empty(t, pub.got, "span-covered native emotes rescue the same line")
}
