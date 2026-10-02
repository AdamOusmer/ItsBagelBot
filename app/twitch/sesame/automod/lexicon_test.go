// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const harassmentLine = "nobody asked just go kill yourself already dude seriously"

func leetspeak(s string) string {
	return strings.NewReplacer("a", "4", "e", "3", "i", "1", "o", "0", "s", "5").Replace(s)
}

func TestLexiconVerdicts(t *testing.T) {
	sexual := "check out this hentai stream it is really something else"
	tests := []struct {
		name        string
		cfg         string
		line        string
		wantAction  Action
		wantSeconds uint32
		wantRule    string
	}{
		{"times out a hate term for thirty minutes", "", "you are such a {slur} lol", ActionTimeout, 1800, "lex:hate:"},
		{"keeps the hate floor immovable", `{"profile":"adult","allow_terms":"{slur}"}`, "you are such a {slur} lol", ActionTimeout, 1800, "lex:hate:"},
		{"catches a bare short slur", "", "{slur}", ActionTimeout, 1800, "lex:hate:"},
		{"catches a leet-obfuscated slur", "", "you are such a {leet} lol", ActionTimeout, 1800, "lex:hate:"},
		{"passes a short clean line", "", "nice clip", ActionNone, 0, ""},
		{"warns on harassment", "", harassmentLine, ActionWarn, 0, "lex:harassment:"},
		{"lets an allow term suppress harassment", `{"allow_terms":"kill yourself"}`, harassmentLine, ActionNone, 0, ""},
		{"deletes sexual terms under moderate", "", sexual, ActionDelete, 0, "lex:sexual:"},
		{"passes sexual terms under adult", `{"profile":"adult"}`, sexual, ActionNone, 0, ""},
		{"passes profanity under moderate", "", profaneLine, ActionNone, 0, ""},
		{"deletes profanity under pg", `{"profile":"pg"}`, profaneLine, ActionDelete, 0, "lex:profanity:"},
		{"matches on word boundaries only", `{"profile":"pg"}`, "the class assignment about cocktail recipes is due tomorrow evening", ActionNone, 0, ""},
		{
			"does not judge non-latin chat by the english lists", "",
			"Привет всем как дела сегодня отличный стрим спасибо за игру друзья", ActionNone, 0, "",
		},
	}
	g := New()
	slur := floorTerm(t)
	fill := strings.NewReplacer("{slur}", slur, "{leet}", leetspeak(slur))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ParseConfig(codec.RawMessage(fill.Replace(tt.cfg)))

			v := g.InspectWith(module.RoleEveryone, fill.Replace(tt.line), cfg)

			assert.Equal(t, tt.wantAction, v.Action)
			assert.Equal(t, tt.wantSeconds, v.Seconds)
			assert.True(t, strings.HasPrefix(v.Rule, tt.wantRule), "rule %q, want prefix %q", v.Rule, tt.wantRule)
		})
	}
}

func TestLexiconDirOverrideAndFallback(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "harassment.txt"), []byte("touch grass\n"), 0o644))
	l, err := LoadLexiconDir(dir)
	require.NoError(t, err)
	g := New()
	g.SetLexicon(l)

	assert.Equal(t, ActionWarn, g.Inspect(module.RoleEveryone, "why dont you go touch grass instead of typing here").Action, "override term")
	assert.Equal(t, ActionNone, g.Inspect(module.RoleEveryone, harassmentLine).Action, "a replaced category drops its old terms")
	assert.Equal(t, ActionTimeout, g.Inspect(module.RoleEveryone, "you are such a "+floorTerm(t)+" lol").Action, "the fallback hate list stays")

	_, err = LoadLexiconDir(filepath.Join(dir, "nope"))
	require.Error(t, err)

	g.SetLexicon(nil)
	assert.Equal(t, ActionWarn, g.Inspect(module.RoleEveryone, harassmentLine).Action, "nil restores the embedded lexicon")
}
