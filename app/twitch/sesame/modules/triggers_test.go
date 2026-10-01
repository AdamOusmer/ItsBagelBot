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
	h := Triggers(engine.Deps{Log: zap.NewNop()}).Events["channel.chat.message"]
	require.NotNil(t, h, "triggers must handle channel.chat.message")
	return h
}

func triggersCtx(text, rules string) *module.Context {
	c := &module.Context{
		Env: lane.Envelope{
			Type:                 "channel.chat.message",
			Text:                 text,
			BroadcasterUserID:    "2",
			BroadcasterUserLogin: "bagel_stream",
			ChatterUserName:      "Bob",
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
	if rules != "" {
		c.Config, _ = codec.Marshal(triggersConfig{Rules: rules})
	}
	return c
}

func quote(s string) string {
	b, _ := codec.Marshal(s)
	return string(b)
}

func TestTriggersChat(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		rules   string
		senders bool
		want    []string
	}{
		{name: "a word rule matches inside a sentence", text: "oh hello there", rules: "hello => hi {user}!", want: []string{"hi Bob!"}},
		{name: "a word rule needs a whole word", text: "watch hellovision", rules: "hello => hi"},
		{name: "matching is case insensitive", text: "HELLO everyone", rules: "hello => hi {user}!", want: []string{"hi Bob!"}},
		{name: "contains mode matches inside a word", text: "hahalolhaha", rules: "contains: lol => lmao", want: []string{"lmao"}},
		{name: "exact mode matches the whole message", text: "gg", rules: "exact: gg => good game", want: []string{"good game"}},
		{name: "exact mode rejects extra words", text: "gg wp", rules: "exact: gg => good game"},
		{name: "prefix mode matches the start", text: "gm chat", rules: "prefix: gm => morning", want: []string{"morning"}},
		{name: "commands are skipped", text: "!hello", rules: "contains: hello => hi"},
		{name: "folded duplicate cohorts are skipped", text: "hello", rules: "hello => hi", senders: true},
		{name: "the first matching rule wins", text: "hi there", rules: "hi => one\nthere => two", want: []string{"one"}},
		{name: "no config is a no-op", text: "hello"},
		{name: "namespaced user and channel tokens render", text: "hello", rules: "hello => Hi {triggers:user}, welcome to {triggers:channel}!",
			want: []string{"Hi Bob, welcome to bagel_stream!"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := triggersCtx(tc.text, tc.rules)
			if tc.senders {
				c.Env.Senders = []lane.Sender{{ChatterUserID: "9"}}
			}
			var col collector
			require.NoError(t, triggersHandler(t)(context.Background(), c, col.emit))
			assert.Equal(t, tc.want, texts(col.out))
			for _, o := range col.out {
				assert.Equal(t, outgress.TypeChat, o.Type)
				assert.Equal(t, "2", o.BroadcasterID)
			}
		})
	}
}

func TestTriggersDistinctConfigsDoNotCollide(t *testing.T) {
	h := triggersHandler(t)
	answerTo := func(rules string) []string {
		var col collector
		require.NoError(t, h(context.Background(), triggersCtx("hello", rules), col.emit))
		return texts(col.out)
	}
	assert.Equal(t, []string{"from A"}, answerTo("hello => from A"))
	assert.Equal(t, []string{"from B"}, answerTo("hello => from B"))
	assert.Equal(t, []string{"from A"}, answerTo("hello => from A"))
}

func TestTriggersMalformedConfigStillErrors(t *testing.T) {
	c := triggersCtx("hello", "x => y")
	c.Config = []byte(`{"rules":`)
	var col collector
	require.Error(t, triggersHandler(t)(context.Background(), c, col.emit))
	assert.Empty(t, col.out)
	assert.Error(t, triggersHandler(t)(context.Background(), c, col.emit))
}

func TestTriggersLoadLocaleOnlyOnMatchAndKeepLocalizedTokens(t *testing.T) {
	calls := 0
	c := triggersCtx("ordinary unmatched prose", "hello => {countup:2020-01-01}")
	c.LocaleLookup = func(context.Context, uint64) (string, error) { calls++; return "fr", nil }
	var col collector
	require.NoError(t, triggersHandler(t)(context.Background(), c, col.emit))
	require.Zero(t, calls)
	c.Env.Text = "hello"
	require.NoError(t, triggersHandler(t)(context.Background(), c, col.emit))
	require.Equal(t, 1, calls)
	require.Len(t, col.out, 1)
	french := module.KV().WithLocale(module.Locale("fr")).ExpandString("{countup:2020-01-01}")
	english := module.KV().WithLocale(module.Locale("en")).ExpandString("{countup:2020-01-01}")
	require.Equal(t, french, col.out[0].Text)
	require.NotEqual(t, english, col.out[0].Text)
}

func TestTriggerRuleSyntax(t *testing.T) {
	var overCap strings.Builder
	for i := range maxTriggers + 10 {
		fmt.Fprintf(&overCap, "w%d => r%d\n", i, i)
	}
	legacy := "# a comment\n\nhello => hi {user}!\n  contains: lol =>  lmao \nnoseparator here\nempty => \n => noPhrase"
	structured := `[
		{"phrase":"hello","response":"hi {user}!","match":"word","enabled":true},
		{"phrase":"lol","response":"lmao","match":"contains","enabled":true},
		{"phrase":"muted","response":"x","match":"word","enabled":false}]`
	reserved := `[{"phrase":` + quote("a => b") + `,"response":"ok","match":"word"},{"phrase":"#hashtag","response":"ok","match":"contains"},{"phrase":"word: literal","response":"ok","match":"prefix"}]`
	cases := []struct {
		name  string
		rules string
		text  string
		want  []string
	}{
		{"legacy text keeps a word rule", legacy, "hello", []string{"hi Bob!"}},
		{"legacy text keeps a contains rule with trimmed parts", legacy, "xlolx", []string{"lmao"}},
		{"legacy text skips a comment line", legacy, "a comment", nil},
		{"legacy text skips a line without a separator", legacy, "noseparator here", nil},
		{"legacy text skips an empty response", legacy, "empty", nil},
		{"legacy text skips an empty phrase", legacy, "noPhrase", nil},
		{"an unknown mode is part of the phrase", "time:30 => later", "time:30", []string{"later"}},
		{"structured rules keep enabled entries", structured, "hello", []string{"hi Bob!"}},
		{"structured rules skip disabled entries", structured, "muted", nil},
		{"structured rules honour the contains mode", structured, "xlolx", []string{"lmao"}},
		{"a separator inside a phrase is preserved", reserved, "a => b", []string{"ok"}},
		{"a hash inside a phrase is preserved", reserved, "xx#hashtagxx", []string{"ok"}},
		{"a colon inside a phrase is preserved", reserved, "word: literal and more", []string{"ok"}},
		{"an unknown structured mode defaults to word", `[{"phrase":"hi","response":"yo","match":"bogus"}]`, "oh hi", []string{"yo"}},
		{"an unknown structured mode is not a substring match", `[{"phrase":"hi","response":"yo","match":"bogus"}]`, "ohhi", nil},
		{"malformed structured rules yield none", `[{"phrase":`, "hello", nil},
		{"the last rule under the cap still fires", overCap.String(), fmt.Sprintf("w%d", maxTriggers-1), []string{fmt.Sprintf("r%d", maxTriggers-1)}},
		{"rules beyond the cap are dropped", overCap.String(), fmt.Sprintf("w%d", maxTriggers), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var col collector
			require.NoError(t, triggersHandler(t)(context.Background(), triggersCtx(tc.text, tc.rules), col.emit))
			assert.Equal(t, tc.want, texts(col.out))
		})
	}
}

func benchTriggerRules() string {
	var b strings.Builder
	for i := 0; i < maxTriggers; i++ {
		fmt.Fprintf(&b, "phrase number %d => response number %d\n", i, i)
	}
	return b.String()
}

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
