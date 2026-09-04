// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newAutomodPipeline(pub bus.Publisher, reader projection.Reader, enforce bool) *Pipeline {
	d := Deps{
		Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(), Automod: automod.New(),
	}
	cfg := Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj, AutomodEnforce: enforce}
	return NewPipeline(d, NewRegistry(zap.NewNop()), cfg)
}

func ipLoggerChat(t *testing.T) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                "claim your prize over at grabify.link/xyz now everyone hurry",
	})
	require.NoError(t, err)
	return bus.NewMessage("u", body)
}

func TestAutomodEnforceEmitsTimeout(t *testing.T) {
	pub := &fakePublisher{}
	p := newAutomodPipeline(pub, fakeReader{}, true)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1, "enforcement should emit one moderation action")
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type)
	assert.Equal(t, "123", pub.got[0].msg.BroadcasterID)
}

func TestAutomodEnforceSkipsCommandDispatch(t *testing.T) {
	pub := &fakePublisher{}
	// The chatter also runs a custom command; enforcement must action them and
	// NOT answer the command (only the timeout is emitted, not a chat reply).
	reader := fakeReader{cmd: projection.Command{Name: "x", Response: "hi", IsActive: true}, cmdFound: true}
	p := newAutomodPipeline(pub, reader, true)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeTimeout, pub.got[0].msg.Type, "only the action, no command reply")
}

func TestAutomodShadowDoesNotAct(t *testing.T) {
	pub := &fakePublisher{}
	p := newAutomodPipeline(pub, fakeReader{}, false)

	require.NoError(t, p.Process(ipLoggerChat(t)))
	assert.Empty(t, pub.got, "shadow mode must emit no action")
}

func TestSanitizeVarStripsLeadingSlash(t *testing.T) {
	assert.Equal(t, "ban someone", sanitizeVar("/ban someone"))
	assert.Equal(t, "ban", sanitizeVar("  //ban"))
	assert.Equal(t, "http://example.com", sanitizeVar("http://example.com"))
	assert.Equal(t, "hello", sanitizeVar("hello"))
}

// A hostile upstream value must not mint extra chat lines or poison rendering:
// control bytes (C0 + DEL) vanish, leading slash runs still trim, multibyte
// runes survive byte-wise filtering (continuations are >= 0x80 and untouched),
// and the ExternalVar cap stays rune-safe on top.
func TestSanitizeVarStripsControlBytes(t *testing.T) {
	assert.Equal(t, "hi/ban everyone", sanitizeVar("hi\r\n/ban everyone"), "no newline survives to mint a second line")
	assert.Equal(t, "clean", sanitizeVar("cle\x00an"))
	assert.Equal(t, "[31mred[0m", sanitizeVar("\x1b[31mred\x1b[0m"))
	assert.Equal(t, "héllowörld", sanitizeVar("héllo\r\nwörld"), "byte-wise strip must not split runes")
	assert.Equal(t, "", sanitizeVar("\r\n\x00\x1b"))

	got := ExternalVar("/me\r\n" + strings.Repeat("é", 120))
	assert.NotContains(t, got, "\r")
	assert.NotContains(t, got, "\n")
	assert.LessOrEqual(t, len(got), MaxExternalVarBytes+2, "cap backs off at most one rune boundary")
}
