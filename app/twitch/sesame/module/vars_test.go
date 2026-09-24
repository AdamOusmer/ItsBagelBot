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
	const in = "{raider} raided with {viewers}! {unknown}"
	const want = "CoolStreamer raided with 42! {unknown}"

	for _, tc := range []struct {
		name string
		run  func() string
	}{
		{"Expand", func() string { return string(Expand(nil, in, repl)) }},
		{"ExpandString", func() string { return ExpandString(in, repl) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, want, tc.run())
		})
	}
}

func TestExpandKeyCaseInsensitive(t *testing.T) {
	repl := func(tok tmpl.Token) (string, bool) {
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
	got := ExpandString("{Random:5-5}", tmpl.Dynamic)
	assert.Equal(t, "5", got)
}

func TestStringPaletteExpand(t *testing.T) {
	p := StringPalette{"player": "Feinberg", "elo": "unrated"}
	assert.Equal(t, "Feinberg: unrated · {rank}", p.Expand("{player}: {elo} · {rank}"))
	assert.Equal(t, "5", p.Expand("{Random:5-5}"))
}

func TestStringPaletteMerge(t *testing.T) {
	base := StringPalette{"player": "Feinberg", "elo": "1650"}
	got := base.Merge(StringPalette{"elo": "1700"}, StringPalette{"rank": "12"})
	assert.Equal(t, StringPalette{"player": "Feinberg", "elo": "1700", "rank": "12"}, got)
	assert.Equal(t, StringPalette{"player": "Feinberg", "elo": "1650"}, base)
}

func TestKVOddLengthDropsDanglingName(t *testing.T) {
	p := KV("a", "1", "dangling")
	assert.Equal(t, "1 {dangling}", p.ExpandString("{a} {dangling}"))
}

func TestKVDuplicateNameLastWins(t *testing.T) {
	p := KV("a", "1", "a", "2")
	assert.Equal(t, "2", p.ExpandString("{a}"))
	assert.Equal(t, []string{"a"}, p.Names(), "a duplicate name must not appear twice in Names()")
}

func TestKVEmptyNameDropped(t *testing.T) {
	p := KV("", "x", "b", "2")
	assert.Equal(t, []string{"b"}, p.Names())
	assert.Equal(t, "2", p.ExpandString("{b}"))
}

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

func TestPaletteWithLocaleWordsCountdownInFrench(t *testing.T) {
	en := KV("user", "sam").ExpandString("{countdown:9999-01-01}")
	fr := KV("user", "sam").WithLocale("fr").ExpandString("{countdown:9999-01-01}")
	assert.NotEqual(t, en, fr, "WithLocale(\"fr\") must change how {countdown} words itself")
}
