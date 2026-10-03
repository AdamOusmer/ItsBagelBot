// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	runes := ExternalVar("/me\r\n" + strings.Repeat("é", 120))
	assert.NotContains(t, runes, "\r")
	assert.NotContains(t, runes, "\n")
	assert.LessOrEqual(t, len(runes), MaxExternalVarBytes+2, "cap backs off at most one rune boundary")
}
