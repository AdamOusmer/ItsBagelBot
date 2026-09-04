// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func triggersHandler(t *testing.T) module.EventHandler {
	t.Helper()
	m := Triggers(engine.Deps{Log: zap.NewNop()})
	assert.Equal(t, "triggers", m.Name)
	assert.Equal(t, module.KindOptIn, m.Kind)
	h := m.Events["channel.chat.message"]
	require.NotNil(t, h, "triggers must handle channel.chat.message")
	return h
}

// triggersCtx builds a chat Context whose Config blob carries the given rules
// textarea (empty rules leaves the blob unset).
func triggersCtx(text, rules string) *module.Context {
	c := &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			Text:              text,
			BroadcasterUserID: "2",
			ChatterUserName:   "Bob",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if rules != "" {
		c.Config = rulesBlob(rules)
	}
	return c
}

// rulesBlob marshals a rules string into the {"rules":"…"} Configs blob, matching
// what the dashboard persists. Marshaling a single string field never errors.
func rulesBlob(rules string) []byte {
	b, _ := codec.Marshal(triggersConfig{Rules: rules})
	return b
}

func TestTriggersWordMatch(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("oh hello there", "hello => hi {user}!"), col.emit))
	require.Len(t, col.out, 1)
	o := col.out[0]
	assert.Equal(t, outgress.TypeChat, o.Type)
	assert.Equal(t, "2", o.BroadcasterID)
	assert.Equal(t, "hi Bob!", o.Text)
}

func TestTriggersWordMatchIsWholeWord(t *testing.T) {
	var col collector
	// "hello" must not fire on "hellovision" under the default word mode.
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("watch hellovision", "hello => hi"), col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersCaseInsensitive(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("HELLO everyone", "hello => hi {user}!"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "hi Bob!", col.out[0].Text)
}

func TestTriggersContainsMode(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hahalolhaha", "contains: lol => lmao"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "lmao", col.out[0].Text)
}

func TestTriggersExactMode(t *testing.T) {
	rules := "exact: gg => good game"

	var hit collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gg", rules), hit.emit))
	require.Len(t, hit.out, 1)
	assert.Equal(t, "good game", hit.out[0].Text)

	var miss collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gg wp", rules), miss.emit))
	assert.Empty(t, miss.out)
}

func TestTriggersPrefixMode(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("gm chat", "prefix: gm => morning"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "morning", col.out[0].Text)
}

func TestTriggersSkipsCommands(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("!hello", "contains: hello => hi"), col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersSkipsCohorts(t *testing.T) {
	c := triggersCtx("hello", "hello => hi")
	c.Env.Senders = []lane.Sender{{ChatterUserID: "9"}}
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), c, col.emit))
	assert.Empty(t, col.out)
}

func TestTriggersFirstMatchWins(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hi there", "hi => one\nthere => two"), col.emit))
	require.Len(t, col.out, 1)
	assert.Equal(t, "one", col.out[0].Text)
}

func TestTriggersNoConfigNoOp(t *testing.T) {
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx("hello", ""), col.emit))
	assert.Empty(t, col.out)
}

// TestParseRules exercises the textarea parser directly: comments, blanks, mode
// prefixes, and malformed lines are all handled.
func TestParseRules(t *testing.T) {
	raw := "# a comment\n\nhello => hi {user}!\n  contains: lol =>  lmao \nnoseparator here\nempty => \n => noPhrase"
	rules := triggersConfig{Rules: raw}.rules()
	require.Len(t, rules, 2)

	assert.Equal(t, "hello", rules[0].Phrase)
	assert.Equal(t, "hi {user}!", rules[0].Response)
	assert.Equal(t, "word", rules[0].Match)

	assert.Equal(t, "lol", rules[1].Phrase)
	assert.Equal(t, "lmao", rules[1].Response)
	assert.Equal(t, "contains", rules[1].Match)
}

// TestParseRulesUnknownMode keeps a colon that is not a real mode as part of the
// phrase rather than dropping it.
func TestParseRulesUnknownMode(t *testing.T) {
	rules := triggersConfig{Rules: "time:30 => later"}.rules()
	require.Len(t, rules, 1)
	assert.Equal(t, "time:30", rules[0].Phrase)
	assert.Equal(t, "word", rules[0].Match)
}

// TestParseRulesCap stops at maxTriggers.
func TestParseRulesCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxTriggers+10; i++ {
		fmt.Fprintf(&b, "w%d => r%d\n", i, i)
	}
	assert.Len(t, triggersConfig{Rules: b.String()}.rules(), maxTriggers)
}

// TestRulesJSONStructured covers the structured JSON format the dashboard now
// writes: it parses like the legacy lines, but a phrase containing the legacy
// delimiters ("=>", a leading "#", a "mode:" prefix) survives the round trip
// instead of being corrupted.
func TestRulesJSONStructured(t *testing.T) {
	raw := `[
		{"phrase":"hello","response":"hi {user}!","match":"word","enabled":true},
		{"phrase":"lol","response":"lmao","match":"contains","enabled":true},
		{"phrase":"muted","response":"x","match":"word","enabled":false}
	]`
	rules := triggersConfig{Rules: raw}.rules()
	require.Len(t, rules, 2, "disabled rule is skipped")
	assert.Equal(t, "hello", rules[0].Phrase)
	assert.Equal(t, "hi {user}!", rules[0].Response)
	assert.Equal(t, "word", rules[0].Match)
	assert.Equal(t, "contains", rules[1].Match)
}

func TestRulesJSONPreservesReservedCharacters(t *testing.T) {
	// Each of these phrases is unrepresentable in the legacy line format.
	cases := []struct{ phrase, match string }{
		{"a => b", "word"},          // contains the "=>" delimiter
		{"#hashtag", "contains"},    // leading "#" (a legacy disabled marker)
		{"word: literal", "prefix"}, // a legacy "mode:" prefix
	}
	for _, tc := range cases {
		raw := `[{"phrase":` + quote(tc.phrase) + `,"response":"ok","match":"` + tc.match + `"}]`
		rules := triggersConfig{Rules: raw}.rules()
		require.Len(t, rules, 1, "phrase %q must round-trip", tc.phrase)
		assert.Equal(t, tc.phrase, rules[0].Phrase, "phrase preserved verbatim")
		assert.Equal(t, "ok", rules[0].Response)
		assert.Equal(t, tc.match, rules[0].Match)
	}
}

func TestRulesJSONDefaults(t *testing.T) {
	// enabled omitted -> enabled; unknown match -> "word".
	rules := triggersConfig{Rules: `[{"phrase":"hi","response":"yo","match":"bogus"}]`}.rules()
	require.Len(t, rules, 1)
	assert.Equal(t, "word", rules[0].Match)

	// malformed JSON fails closed (no rules), like an empty config.
	assert.Nil(t, triggersConfig{Rules: `[{"phrase":`}.rules())

	// a legacy line config still parses (backward compatibility).
	legacy := triggersConfig{Rules: "hello => hi {user}!\ncontains: lol => lmao"}.rules()
	require.Len(t, legacy, 2)
	assert.Equal(t, "contains", legacy[1].Match)
}

// quote JSON-encodes a string (with surrounding quotes) for embedding in a rule
// array literal.
func quote(s string) string {
	b, _ := codec.Marshal(s)
	return string(b)
}

// TestTriggersDistinctConfigsDoNotCollide is the rules-cache correctness guard.
// Both channels' rows are legacy rows (ModuleView.Revision 0), so a
// revision-keyed cache would answer channel B's chat with channel A's trigger
// response. The cache keys on the blob's content, so each channel gets its own.
func TestTriggersDistinctConfigsDoNotCollide(t *testing.T) {
	h := triggersHandler(t)

	var a collector
	require.NoError(t, h(context.Background(), triggersCtx("hello", "hello => from A"), a.emit))
	var b collector
	require.NoError(t, h(context.Background(), triggersCtx("hello", "hello => from B"), b.emit))
	// Re-read in the opposite order: a hit must still be a hit on its own bytes.
	var a2 collector
	require.NoError(t, h(context.Background(), triggersCtx("hello", "hello => from A"), a2.emit))

	require.Len(t, a.out, 1)
	require.Len(t, b.out, 1)
	require.Len(t, a2.out, 1)
	assert.Equal(t, "from A", a.out[0].Text)
	assert.Equal(t, "from B", b.out[0].Text)
	assert.Equal(t, "from A", a2.out[0].Text)
}

// TestTriggersMalformedConfigStillErrors: the cached parse carries the decode
// error, so a broken blob reaches the engine as a handler error exactly as it
// did when triggersOnChat called Context.Decode per message.
func TestTriggersMalformedConfigStillErrors(t *testing.T) {
	c := triggersCtx("hello", "x => y")
	c.Config = []byte(`{"rules":`) // truncated JSON

	var col collector
	err := triggersHandler(t)(context.Background(), c, col.emit)
	require.Error(t, err)
	assert.Empty(t, col.out)
	// The error is cached with the blob; a second read must report it too.
	assert.Error(t, triggersHandler(t)(context.Background(), c, col.emit))
}

// benchTriggerRules is a full-size rule set (maxTriggers entries): the config a
// broadcaster who leans on the module actually saves, and the worst case the
// per-message parse used to pay for.
func benchTriggerRules() string {
	var b strings.Builder
	for i := 0; i < maxTriggers; i++ {
		fmt.Fprintf(&b, "phrase number %d => response number %d\n", i, i)
	}
	return b.String()
}

// BenchmarkTriggersOnChatMiss measures the handler on a line that matches
// nothing, which is what nearly every chat line does: before the rules cache
// this decoded the blob and rebuilt all 50 rules per message.
func BenchmarkTriggersOnChatMiss(b *testing.B) {
	c := triggersCtx("just chatting about the stream", benchTriggerRules())
	emit := func(*module.Output) {}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := triggersOnChat(context.Background(), c, emit); err != nil {
			b.Fatal(err)
		}
	}
}
