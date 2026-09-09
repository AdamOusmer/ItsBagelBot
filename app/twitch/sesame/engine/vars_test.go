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

// renderScopes runs the two phases a custom command run does — lex, plan the
// chain, render — and returns the chat text. It is the test-side stand-in for
// runCustom + emitResponse, so a table can pin expansion without building a
// pipeline. dst is the caller's buffer, mirroring the pooled one emitResponse
// passes in.
func renderScopes(dst []byte, template string, scopes ...scope.Scope) string {
	toks := tmpl.Lex(template)
	chain := scope.Chain(scopes)
	values := chain.Plan(context.Background(), toks, nil)
	return string(chain.Render(dst, toks, values))
}

// commandScopes is the always-mounted half of a custom command's chain: the
// dice and the triggering line. Counters and urlfetch mount only when their
// dependency is wired, so a table testing the token grammar leaves them out.
func commandScopes() []scope.Scope {
	return []scope.Scope{scope.Pure{}, scope.Message{
		User:    "alice",
		Sender:  "alice",
		Args:    "the rest here",
		Touser:  "bob",
		Channel: "channel_name",
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
		// A payload is not silently ignored: {user:bob} is not a token this
		// palette has, so it stays literal like any other unknown spelling.
		{"identity token with a payload stays literal", "{user:bob}", "{user:bob}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderScopes(nil, tt.tmpl, commandScopes()...))
		})
	}
}

// TestRenderAppendsIntoDst covers the pooled path emitResponse uses: Render
// writes into the caller's buffer rather than returning a fresh one.
func TestRenderAppendsIntoDst(t *testing.T) {
	got := renderScopes([]byte("prefix: "), "hi {user}", commandScopes()...)
	assert.Equal(t, "prefix: hi alice", got)
}

// TestRenderFallbackPipe pins the {token|fallback} grammar at the chain level:
// an empty value falls back, a present value does not, and a fallback never
// rescues a name no mounted scope owns (issue #884).
func TestRenderFallbackPipe(t *testing.T) {
	scopes := []scope.Scope{scope.Pure{}, scope.Message{
		User: "alice", Sender: "alice", Args: "", Touser: "alice", Channel: "chan",
	}}
	tests := []struct{ name, tmpl, want string }{
		{"empty value falls back", "shout out to {args|everyone}", "shout out to everyone"},
		{"present value wins", "hi {user|everyone}", "hi alice"},
		{"empty value with no fallback renders nothing", "hi {args}!", "hi !"},
		{"unknown name keeps its whole span", "{nosuchtoken|rescued}", "{nosuchtoken|rescued}"},
		{"an unmounted scope's token keeps its span", "{counter:deaths|0}", "{counter:deaths|0}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderScopes(nil, tt.tmpl, scopes...))
		})
	}
}

// TestRenderDynamicTokens pins that the pure scope still answers the generic
// dynamic palette, and that repeated {random} spans draw independently — two
// dice in one line have always rolled separately and a per-key plan cache
// would have quietly collapsed them into one number.
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

// TestMessageVarsSanitizesViewerInput pins the injection guard at the boundary
// that mints the viewer-controlled tokens: a crafted argument cannot carry a
// leading slash-verb (or a newline that would mint a fresh line for one) into
// the expansion, and an over-mentioned "@@bob" still reads as "bob".
func TestMessageVarsSanitizesViewerInput(t *testing.T) {
	c := chatCtx("!so", "")
	vars := func(args string) scope.Message { return messageVars(commandRun{c: c, command: "!so", args: args}) }
	got := vars("/ban @everyone")
	assert.Equal(t, "ban @everyone", got.Args)
	assert.Equal(t, "ban", got.Touser)

	assert.Equal(t, "bob", vars("@@bob hi").Touser)
	assert.Equal(t, "alice", vars("").Touser, "no argument: the sender is the target")
	assert.Equal(t, "hithere", vars("hi\nthere").Args, "a newline is stripped, never kept as a line break")
}
