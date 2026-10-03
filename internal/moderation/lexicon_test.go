// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hateLexicon(t *testing.T, terms ...string) *Lexicon {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hate.txt"), []byte(strings.Join(terms, "\n")), 0o644))
	lex, err := LoadLexiconDir(dir)
	require.NoError(t, err)
	return lex
}

func scanHate(lex *Lexicon, text string) (Category, string) {
	return lex.Scan([]byte(" "+text+" "), true)
}

func TestLexiconScanFindsWordBoundedTerms(t *testing.T) {
	lex := hateLexicon(t, "he", "she", "hers", "his", "ass", "kill yourself")
	cases := []struct {
		name string
		text string
		want string
	}{
		{"a short term inside a longer word does not match", "ushers", ""},
		{"overlapping terms resolve to the whole word", "hers", "hers"},
		{"a term matches mid sentence", "he said so", "he"},
		{"a padded term matches standalone", "kick his ass ok", "his"},
		{"a padded term does not match inside a word", "class assignment", ""},
		{"a multi word term matches", "please kill yourself now", "kill yourself"},
		{"no term in the text", "nothing", ""},
		{"empty text", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat, term := scanHate(lex, tc.text)
			assert.Equal(t, tc.want, term)
			assert.Equal(t, tc.want != "", cat == CatHate)
		})
	}
}

func TestLexiconFloorPrescanFoldsLeetAndCase(t *testing.T) {
	lex := hateLexicon(t, "ease", "ass")
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"leet digits fold to letters", "34s3", true},
		{"uppercase folds", "EASE", true},
		{"symbols fold to letters", "@$$", true},
		{"punctuation separates words", "an,ease,ok", true},
		{"a term inside a longer word does not match", "classy assignment", false},
		{"non ascii bytes never fold into a term", "eéase", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, lex.FloorPrescan(tc.text))
		})
	}
}

func TestLexiconScanDoesNotAllocate(t *testing.T) {
	lex := hateLexicon(t, "kill yourself", "kys")
	text := []byte(" a totally normal long chat message about the game we are watching ")

	assert.Zero(t, testing.AllocsPerRun(200, func() { lex.Scan(text, true) }))
}

func TestLexiconAgreesWithNaiveSubstringSearch(t *testing.T) {
	termSets := [][]string{
		{"he", "she", "hers", "his"},
		{"ass", "kys", "kill yourself"},
		{"free nitro", "nitro"},
		{"a"},
		{"abc", "abcabc"},
	}
	lexicons := make([]*Lexicon, len(termSets))
	for i, terms := range termSets {
		lexicons[i] = hateLexicon(t, terms...)
	}
	rng := rand.New(rand.NewSource(20260822))
	for i := 0; i < 20000; i++ {
		terms := termSets[i%len(termSets)]
		lex := lexicons[i%len(termSets)]
		alphabet := []byte(strings.Join(terms, "") + " ")
		text := make([]byte, rng.Intn(24))
		for j := range text {
			text[j] = alphabet[rng.Intn(len(alphabet))]
		}
		want := naiveContainsTerm(string(text), terms)

		_, scanned := scanHate(lex, string(text))
		assert.Equal(t, want, scanned != "", "Scan(%q) against %v", text, terms)
		assert.Equal(t, want, lex.FloorPrescan(string(text)), "FloorPrescan(%q) against %v", text, terms)
	}
}

func naiveContainsTerm(text string, terms []string) bool {
	padded := " " + text + " "
	for _, term := range terms {
		if strings.Contains(padded, " "+term+" ") {
			return true
		}
	}
	return false
}
