// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
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
