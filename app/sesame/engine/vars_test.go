package engine

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
		{"target alias", "@{target} pong", "@bob pong"},
		{"multiple tokens", "{user} -> {touser}: {args}", "alice -> bob: the rest here"},
		{"unknown token preserved", "keep {whatever} intact", "keep {whatever} intact"},
		{"unterminated brace literal", "dangling {user and {more", "dangling {user and {more"},
		{"first brace closes span", "{user} and {more", "alice and {more"},
		{"adjacent tokens", "{user}{touser}", "alicebob"},
		{"empty braces preserved", "a {} b", "a {} b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandCommand(nil, tt.tmpl, tokens{user: "alice", sender: "alice", args: "the rest here", touser: "bob", channel: "channel_name"})
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestExpandAppendsIntoDst(t *testing.T) {
	dst := []byte("prefix: ")
	got := expandCommand(dst, "hi {user}", tokens{user: "alice", sender: "alice", touser: "alice", channel: "channel_name"})
	assert.Equal(t, "prefix: hi alice", string(got))
}
