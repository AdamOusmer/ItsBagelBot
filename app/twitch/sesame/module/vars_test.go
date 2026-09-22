// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"ItsBagelBot/pkg/tmpl"
)

func TestExpandGenericRepl(t *testing.T) {
	repl := func(tok tmpl.Token) (string, bool) {
		switch tok.Key() {
		case "raider":
			return "CoolStreamer", true
		case "viewers":
			return "42", true
		default:
			return "", false
		}
	}
	got := Expand(nil, "{raider} raided with {viewers}! {unknown}", repl)
	assert.Equal(t, "CoolStreamer raided with 42! {unknown}", string(got))
}

func TestExpandString(t *testing.T) {
	repl := func(tok tmpl.Token) (string, bool) {
		switch tok.Key() {
		case "raider":
			return "CoolStreamer", true
		case "viewers":
			return "42", true
		default:
			return "", false
		}
	}
	got := ExpandString("{raider} raided with {viewers}! {unknown}", repl)
	assert.Equal(t, "CoolStreamer raided with 42! {unknown}", got)
}

func TestExpandKeyCaseInsensitive(t *testing.T) {
	repl := func(tok tmpl.Token) (string, bool) {
		// Keys arrive with the name lowercased; the payload keeps its case.
		switch key := tok.Key(); {
		case key == "user":
			return "sam", true
		case key == "choice:Hi,Yo":
			return "Hi", true
		default:
			return "", false
		}
	}
	got := ExpandString("{User} says {CHOICE:Hi,Yo} to {USER}", repl)
	assert.Equal(t, "sam says Hi to sam", got)
}

func TestDynamicCaseInsensitiveViaExpand(t *testing.T) {
	// {Random:5-5} lexes to name "random", payload "5-5" before Dynamic sees
	// it, so the fold is the lexer's and not repeated here.
	got := ExpandString("{Random:5-5}", tmpl.Dynamic)
	assert.Equal(t, "5", got)
}

// TestStringPaletteExpand pins that a palette resolves the same three ways a
// TokenExpander does: its own value, then the dynamic vars, then literal.
func TestStringPaletteExpand(t *testing.T) {
	p := StringPalette{"player": "Feinberg", "elo": "unrated"}
	assert.Equal(t, "Feinberg: unrated · {rank}", p.Expand("{player}: {elo} · {rank}"))
	assert.Equal(t, "5", p.Expand("{Random:5-5}"))
}

// TestStringPaletteMerge covers what the merged fragments rely on: later parts
// win, and the receiver is left alone so a shared fragment cannot pick up one
// command's extra tokens.
func TestStringPaletteMerge(t *testing.T) {
	base := StringPalette{"player": "Feinberg", "elo": "1650"}
	got := base.Merge(StringPalette{"elo": "1700"}, StringPalette{"rank": "12"})
	assert.Equal(t, StringPalette{"player": "Feinberg", "elo": "1700", "rank": "12"}, got)
	assert.Equal(t, StringPalette{"player": "Feinberg", "elo": "1650"}, base)
}

// TestKVOddLengthDropsDanglingName pins the pre-Palette chatReplier.reply
// behaviour: an unpaired trailing name (a caller bug, not a template one) is
// silently dropped rather than resolving to "" or panicking.
func TestKVOddLengthDropsDanglingName(t *testing.T) {
	p := KV("a", "1", "dangling")
	assert.Equal(t, "1 {dangling}", p.ExpandString("{a} {dangling}"))
}

// TestKVDuplicateNameLastWins matches raffle_mechanics' pre-Palette
// map[string]string (a later key overwrites an earlier one) and Merge's own
// later-wins rule: KV must not silently prefer the first pair it saw.
func TestKVDuplicateNameLastWins(t *testing.T) {
	p := KV("a", "1", "a", "2")
	assert.Equal(t, "2", p.ExpandString("{a}"))
	assert.Equal(t, []string{"a"}, p.Names(), "a duplicate name must not appear twice in Names()")
}

// TestKVEmptyNameDropped: no lexed Token ever has an empty Name, so a pair
// shaped that way can never resolve; KV drops it rather than carrying a dead
// entry.
func TestKVEmptyNameDropped(t *testing.T) {
	p := KV("", "x", "b", "2")
	assert.Equal(t, []string{"b"}, p.Names())
	assert.Equal(t, "2", p.ExpandString("{b}"))
}

// TestPaletteRandomDiffersPerSpan pins Resolve's render-time evaluation of
// the pure family (engine/scope/pure.go's pureValues.Get, reached through
// Palette.Resolve's miss path): "{random} and {random}" must be free to
// print two different numbers, the same guarantee a custom command's own
// template has. A Plan-time cache (resolving once per key) would make both
// spans print the same number, which is the one regression pure.go's own
// decision record calls out by name.
func TestPaletteRandomDiffersPerSpan(t *testing.T) {
	p := KV("user", "sam")
	seenDifferent := false
	for i := 0; i < 50 && !seenDifferent; i++ {
		got := p.ExpandString("{random:1-1000000} {random:1-1000000}")
		var a, b int
		n, err := fmt.Sscanf(got, "%d %d", &a, &b)
		assert.NoError(t, err)
		assert.Equal(t, 2, n)
		if a != b {
			seenDifferent = true
		}
	}
	assert.True(t, seenDifferent, "two {random} spans in one template never differed across 50 renders")
}

// TestPaletteWithLocaleWordsCountdownInFrench pins that a palette built off
// KV (which starts with no locale) can still opt into the channel's locale
// for the pure family's humanizer, same as one built off Common already
// does automatically.
func TestPaletteWithLocaleWordsCountdownInFrench(t *testing.T) {
	en := KV("user", "sam").ExpandString("{countdown:9999-01-01}")
	fr := KV("user", "sam").WithLocale("fr").ExpandString("{countdown:9999-01-01}")
	assert.NotEqual(t, en, fr, "WithLocale(\"fr\") must change how {countdown} words itself")
}
