package module

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpandCommand(t *testing.T) {
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
		{"multiple tokens", "{user} -> {touser}: {args}", "alice -> bob: the rest here"},
		{"unknown token preserved", "keep {whatever} intact", "keep {whatever} intact"},
		// No closing brace anywhere after the first '{': the tail is copied literally.
		{"unterminated brace literal", "dangling {user and {more", "dangling {user and {more"},
		// First '}' closes the span; the {user} resolves, the trailing '{more' is literal.
		{"first brace closes span", "{user} and {more", "alice and {more"},
		{"adjacent tokens", "{user}{touser}", "alicebob"},
		{"empty braces preserved", "a {} b", "a {} b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandCommand(nil, tt.tmpl, "alice", "alice", "the rest here", "bob")
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestExpandGenericRepl(t *testing.T) {
	// The generic callback form is what shoutout reuses (raider/viewers tokens).
	repl := func(key string) (string, bool) {
		switch key {
		case "raider":
			return "CoolStreamer", true
		case "viewers":
			return "42", true
		default:
			return "", false
		}
	}
	got := expand(nil, "{raider} raided with {viewers}! {unknown}", repl)
	assert.Equal(t, "CoolStreamer raided with 42! {unknown}", string(got))
}

func TestExpandAppendsIntoDst(t *testing.T) {
	dst := []byte("prefix: ")
	got := expandCommand(dst, "hi {user}", "alice", "alice", "", "alice")
	assert.Equal(t, "prefix: hi alice", string(got))
}
