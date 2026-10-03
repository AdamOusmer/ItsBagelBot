// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func argsChain() Chain {
	return Chain{Message{
		User: "sam", Sender: "sam",
		Words:  []string{"kim", "good", "luck"},
		Touser: "kim", Channel: "bakery",
		UserID: "4242", Login: "sam_login", Command: "hug",
	}}
}

func TestMessageFields(t *testing.T) {
	plain := Chain{Message{User: "sam", Sender: "sam", Touser: "kim", Channel: "bakery"}}
	tests := []struct {
		name     string
		chain    Chain
		template string
		want     string
	}{
		{"renders the sender and target fields", plain, "{user} {sender} {touser} {target} {channel}", "sam sam kim kim bakery"},
		{"rejects a payload on a field", plain, "{user:sam}", "{user:sam}"},
		{"falls back for empty args", plain, "{args|everyone}", "everyone"},
		{"renders the identity tokens", argsChain(), "{user.id} {user.login} {command}", "4242 sam_login hug"},
		{"keeps the legacy user id alias", argsChain(), "{userid}", "4242"},
		{
			name:     "escapes the whole argument string for a query",
			chain:    Chain{Pure{}, Message{Words: []string{"hello", "world", "&", "friends"}}},
			template: "{querystring} {queryescape:hello world & friends}",
			want:     "hello+world+%26+friends hello+world+%26+friends",
		},
		{
			name:     "renders an empty query string without arguments",
			chain:    Chain{Message{}},
			template: "[{querystring}] [{querystring|none}]",
			want:     "[] [none]",
		},
		{
			name:     "rejects a payload on the query string",
			chain:    Chain{Message{Words: []string{"hi"}}},
			template: "{querystring:x}",
			want:     "{querystring:x}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, tt.chain, nil))
		})
	}
}

func TestMessagePositionalWords(t *testing.T) {
	tests := []struct{ name, template, want string }{
		{"reads the first word", "{1}", "kim"},
		{"reads the last word", "{3}", "luck"},
		{"reads the rest from a word", "{2:}", "good luck"},
		{"reads every word", "{1:}", "kim good luck"},
		{"renders nothing past the last word", "[{4}]", "[]"},
		{"renders nothing for the rest past the last word", "[{4:}]", "[]"},
		{"renders the fallback past the last word", "{4|nobody}", "nobody"},
		{"renders nothing for the highest positional", "{30}", ""},
		{"reads a bounded slice", "{1:2}", "kim good"},
		{"reads the tail of a bounded slice", "{2:3}", "good luck"},
		{"clamps a bounded slice to the words present", "{1:30}", "kim good luck"},
		{"reads a leading slice", "{:2}", "kim good"},
		{"clamps a leading slice", "{:30}", "kim good luck"},
		{"renders nothing for a slice past the last word", "[{4:5}]", "[]"},
		{"leaves position zero literal", "{0}", "{0}"},
		{"leaves a position over the cap literal", "{31}", "{31}"},
		{"leaves a signed position literal", "{+1}", "{+1}"},
		{"leaves a zero-padded position literal", "{01}", "{01}"},
		{"leaves a three digit position literal", "{999}", "{999}"},
		{"leaves a backwards slice literal", "{2:1}", "{2:1}"},
		{"leaves a non-numeric slice end literal", "{1:x}", "{1:x}"},
		{"leaves a non-numeric leading slice literal", "{:x}", "{:x}"},
		{"leaves an empty leading slice literal", "{:}", "{:}"},
		{"leaves an empty span literal", "{}", "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, argsChain(), nil))
		})
	}
}
