// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module_test

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/assert"
)

func emoteSet(codes ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	return set
}

func TestEmoteCodesSlicesRawTextByRunes(t *testing.T) {
	c := &module.Context{Env: lane.Envelope{
		Text: "\U0001F389LUL\U0001F389 Cheer100 hey",
		Emotes: []lane.EmoteSpan{
			{ID: "party", Begin: 1, End: 4},
			{ID: "cheer", Begin: 6, End: 14},
			{ID: "neg", Begin: -1, End: 3},
			{ID: "past", Begin: 3, End: 99},
			{ID: "empty", Begin: 2, End: 2},
			{ID: "inverted", Begin: 4, End: 1},
		},
	}}

	assert.Equal(t, emoteSet("lul", "cheer100"), c.EmoteCodes())

	c.Env = lane.Envelope{}
	assert.Equal(t, emoteSet("lul", "cheer100"), c.EmoteCodes(), "codes are built once per message")
}

func TestEmoteCodesWithoutValidSpansAllocatesNothing(t *testing.T) {
	assert.Nil(t, (&module.Context{}).EmoteCodes())

	c := &module.Context{Env: lane.Envelope{Text: "plain chat line", Emotes: []lane.EmoteSpan{{Begin: 0, End: 99}}}}
	assert.Nil(t, c.EmoteCodes())
	assert.Zero(t, testing.AllocsPerRun(100, func() { _ = c.EmoteCodes() }))
}

func TestResetDropsCachedEmoteCodes(t *testing.T) {
	c := &module.Context{Env: lane.Envelope{Text: "LUL", Emotes: []lane.EmoteSpan{{Begin: 0, End: 3}}}}
	assert.Equal(t, emoteSet("lul"), c.EmoteCodes())

	c.Reset()
	c.Env = lane.Envelope{Text: "KEKW", Emotes: []lane.EmoteSpan{{Begin: 0, End: 4}}}

	assert.Equal(t, emoteSet("kekw"), c.EmoteCodes())
}
