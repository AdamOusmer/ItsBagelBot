// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
)

func renderScopes(dst []byte, template string, scopes ...scope.Scope) string {
	toks := tmpl.Lex(template)
	chain := scope.Chain(scopes)
	values := chain.Plan(context.Background(), toks, nil)
	return string(chain.Render(dst, toks, values))
}

func commandScopes() []scope.Scope {
	return []scope.Scope{scope.Pure{}, scope.Message{
		User:    "alice",
		Sender:  "alice",
		Words:   []string{"the", "rest", "here"},
		Touser:  "bob",
		Channel: "channel_name",
		UserID:  "999",
		Login:   "alice_login",
		Command: "hug",
	}}
}

func TestRenderCommandTokens(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{"no tokens", "plain text", "plain text"},
		{"user token", "hi {user}", "hi alice"},
		{"sender alias", "from {sender}", "from alice"},
		{"args token", "you said {args}", "you said the rest here"},
		{"touser token", "@{touser} pong", "@bob pong"},
		{"target alias", "@{target} pong", "@bob pong"},
		{"channel token", "in {channel}", "in channel_name"},
		{"multiple tokens", "{user} -> {touser}: {args}", "alice -> bob: the rest here"},
		{"unknown token preserved", "keep {whatever} intact", "keep {whatever} intact"},
		{"unterminated brace literal", "dangling {user and {more", "dangling {user and {more"},
		{"first brace closes span", "{user} and {more", "alice and {more"},
		{"adjacent tokens", "{user}{touser}", "alicebob"},
		{"empty braces preserved", "a {} b", "a {} b"},
		{"identity token with a payload stays literal", "{user:bob}", "{user:bob}"},
		{"first word", "hug {1}", "hug the"},
		{"second word", "{2}", "rest"},
		{"rest of the args from word 2", "{2:}", "rest here"},
		{"rest from word 1 equals the args", "{1:}", "the rest here"},
		{"a word past the end renders nothing", "[{9}]", "[]"},
		{"a rest past the end renders nothing", "[{9:}]", "[]"},
		{"the last addressable word", "[{30}]", "[]"},
		{"past the cap stays literal", "since {31}", "since {31}"},
		{"a signed number is not a word", "{+1}", "{+1}"},
		{"a padded number is not a word", "{01}", "{01}"},
		{"word zero is not a word", "{0}", "{0}"},
		{"a bounded slice", "{1:2}", "the rest"},
		{"a bounded slice clamps past the end", "{2:5}", "rest here"},
		{"a leading slice", "{:2}", "the rest"},
		{"m before n is a typo, stays literal", "{2:1}", "{2:1}"},
		{"a non-numeric bound is not this grammar", "{1:x}", "{1:x}"},
		{"user id", "id {user.id}", "id 999"},
		{"user id legacy alias", "id {userid}", "id 999"},
		{"login is not the display name", "{user} is {user.login}", "alice is alice_login"},
		{"canonical command name", "!{command}", "!hug"},
		{"a named viewer picks then", "{if:touser:hi there:hi nobody}", "hi there"},
		{"a missing word picks else", "{if:4:word four:no fourth word}", "no fourth word"},
		{"a missing word with no else says nothing", "[{if:4:word four}]", "[]"},
		{"equality against the command name", "{if:command=hug:hugs:waves}", "hugs"},
		{"equality is case-sensitive", "{if:command=HUG:hugs:waves}", "waves"},
		{"a cond on a token this chain does not own stays literal", "{if:points:rich:poor}", "{if:points:rich:poor}"},
		{"the cond keeps its own payload", "{if:2:=rest here:exact:other}", "exact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderScopes(nil, tt.tmpl, commandScopes()...))
		})
	}
}

func TestRenderAppendsIntoDst(t *testing.T) {
	got := renderScopes([]byte("prefix: "), "hi {user}", commandScopes()...)
	assert.Equal(t, "prefix: hi alice", got)
}

type emptyFetcher struct{}

func (emptyFetcher) Fetch(_ context.Context, names []string) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = ""
	}
	return out
}

func TestRenderFallbackPipe(t *testing.T) {
	scopes := []scope.Scope{scope.Pure{}, scope.Message{
		User: "alice", Sender: "alice", Touser: "", Channel: "chan",
	}, scope.External{Fetcher: emptyFetcher{}, Max: 4}}
	tests := []struct{ name, tmpl, want string }{
		{"empty value falls back", "shout out to {args|everyone}", "shout out to everyone"},
		{"present value wins", "hi {user|everyone}", "hi alice"},
		{"empty value with no fallback renders nothing", "hi {args}!", "hi !"},
		{"unknown name keeps its whole span", "{nosuchtoken|rescued}", "{nosuchtoken|rescued}"},
		{"an unmounted scope's token keeps its span", "{counter:deaths|0}", "{counter:deaths|0}"},
		{"a named viewer falls back", "shout out to {touser|nobody}", "shout out to nobody"},
		{"a missing word falls back", "hug {2|none}", "hug none"},
		{"an empty external value falls back", "temp is {urlfetch:x|down}", "temp is down"},
		{"a fallback never rescues a typo", "{unknown|x}", "{unknown|x}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderScopes(nil, tt.tmpl, scopes...))
		})
	}
}

func TestRenderDynamicTokens(t *testing.T) {
	assert.Equal(t, "a", renderScopes(nil, "{choice:a}", commandScopes()...))
	assert.Equal(t, "{choice}", renderScopes(nil, "{choice}", commandScopes()...),
		"a bare {choice} names no options and stays literal")
	assert.Equal(t, "3", renderScopes(nil, "{random:3-3}", commandScopes()...))

	rolls := map[string]struct{}{}
	for range 40 {
		rolls[renderScopes(nil, "{random:1-1000}{random:1-1000}", commandScopes()...)] = struct{}{}
	}
	assert.Greater(t, len(rolls), 1, "two {random} spans must roll independently")
}

func TestMessageVarsSanitizesViewerInput(t *testing.T) {
	c := chatCtx("!so", "")
	got := messageVars(commandRun{c: c, command: "so", args: "/ban @everyone"})
	assert.Equal(t, "ban", got.Words[0], "a leading slash is defanged word by word, not just at the line's own start")
	assert.Equal(t, "ban", got.Touser)

	assert.Equal(t, "bob", messageVars(commandRun{c: c, command: "so", args: "@@bob hi"}).Touser)
	assert.Equal(t, "alice", messageVars(commandRun{c: c, command: "so"}).Touser, "no argument: the sender is the target")
}

func TestMessageVarsSanitizesEveryWord(t *testing.T) {
	got := messageVars(commandRun{c: chatCtx("!so", ""), command: "so", args: "hey /me is a cat"})
	assert.Equal(t, []string{"hey", "me", "is", "a", "cat"}, got.Words)

	assert.Nil(t, messageVars(commandRun{c: chatCtx("!so", ""), command: "so", args: "   "}).Words, "no words, no slots")
	assert.Equal(t, []string{"", "b"}, messageVars(commandRun{c: chatCtx("!so", ""), command: "so", args: "/// b"}).Words,
		"a word that sanitizes away keeps its slot, so later words do not shift")
}

func TestMessageVarsCarriesIdentity(t *testing.T) {
	got := messageVars(commandRun{c: chatCtx("!cuddle", ""), command: "hug"})
	assert.Equal(t, "999", got.UserID)
	assert.Equal(t, "alice", got.Login)
	assert.Equal(t, "hug", got.Command, "an alias resolves to the canonical name")
}
