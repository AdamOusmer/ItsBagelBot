// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
)

func TestSpeechPolicy(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want moderation.SpeechPolicy
	}{
		{"no config follows moderate", nil, moderation.SpeechPolicy{Harassment: true, Sexual: true}},
		{"disabled keeps only the floor", &Config{Disabled: true, blockTerms: normalizeTerms([]string{"x"})}, moderation.SpeechPolicy{}},
		{"none level drops every section", ParseConfig(codec.RawMessage(`{"level":"none"}`)), moderation.SpeechPolicy{}},
		{"strict turns profanity on", ParseConfig(codec.RawMessage(`{"level":"all"}`)), moderation.SpeechPolicy{Harassment: true, Sexual: true, Profanity: true}},
		{"override beats the level", ParseConfig(codec.RawMessage(`{"level":"none","profanity":"on"}`)), moderation.SpeechPolicy{Profanity: true}},
		{"terms are normalized", ParseConfig(codec.RawMessage(`{"level":"none","block_terms":"BadWord","allow_terms":"Fine"}`)),
			moderation.SpeechPolicy{BlockTerms: [][]byte{[]byte("badword")}, AllowTerms: [][]byte{[]byte("fine")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.SpeechPolicy())
		})
	}
}

func TestStripLinks(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text is untouched", "gg well played", "gg well played"},
		{"url is removed", "see https://example.com/a now", "see now"},
		{"bare host is removed", "visit example.com today", "visit today"},
		{"clip link is removed", "watch https://clips.twitch.tv/AbCd-1 lol", "watch lol"},
		{"link only leaves nothing", "https://example.com", ""},
		{"ellipsis stays", "wait... what", "wait... what"},
		{"numbers stay", "scored 3.5 points", "scored 3.5 points"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, StripLinks(tt.in))
		})
	}
}
