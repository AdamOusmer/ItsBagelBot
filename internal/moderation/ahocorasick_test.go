// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestMatcherFindsPatterns(t *testing.T) {
	m := newMatcher([][]byte{[]byte("he"), []byte("she"), []byte("hers"), []byte("his")})

	cases := map[string]int{
		"ushers":  1,  // "she" inside, found via failure links
		"hers":    0,  // "he" hits first (prefix of hers)
		"this":    3,  // "his" via suffix
		"nothing": -1, // "nothing" contains... n-o-t-h-i-n-g: no pattern
		"":        -1,
	}
	for text, want := range cases {
		if got := m.find([]byte(text)); got != want {
			t.Fatalf("find(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestMatcherWordBoundedTerms(t *testing.T) {
	// Lexicon-style space padding: " ass " never matches inside "class".
	m := newMatcher([][]byte{[]byte(" ass ")})
	if m.find([]byte(" class assignment ")) != -1 {
		t.Fatal("padded term must not match inside a word")
	}
	if m.find([]byte(" kick his ass ok ")) != 0 {
		t.Fatal("padded term must match as a standalone word")
	}
}

func TestMatcherZeroAllocFind(t *testing.T) {
	m := newMatcher([][]byte{[]byte(" kill yourself "), []byte(" kys ")})
	text := []byte(" a totally normal long chat message about the game we are watching ")
	allocs := testing.AllocsPerRun(200, func() { _ = m.find(text) })
	if allocs != 0 {
		t.Fatalf("find allocated %.1f/op, want 0", allocs)
	}
}

// TestMatcherDifferentialNaive pins the automaton's two entry points against
// naive scans over a deterministic pseudo-random corpus: find must agree with
// bytes.Contains on "any pattern present", and findFolded (the clean-path
// pre-scan engine) must agree with a fold-then-Contains reference. Pattern
// sets deliberately include prefix/suffix overlaps ("he" in "she"/"hers"),
// which exercise the failure links.
func TestMatcherDifferentialNaive(t *testing.T) {
	patternSets := [][][]byte{
		{[]byte("he"), []byte("she"), []byte("hers"), []byte("his")},
		{[]byte(" ass "), []byte(" kys "), []byte("kill yourself ")},
		{[]byte("free nitro"), []byte("nitro")},
		{[]byte("a")},
		{[]byte("abc"), []byte("abcabc")},
	}

	// Reference for findFolded: apply foldTable to the whole text, pad virtual
	// spaces on both ends exactly as findFolded does, then naive Contains per
	// pattern. Patterns are already written in skeleton space.
	foldedContains := func(text string, pats [][]byte) bool {
		buf := make([]byte, 0, len(text)+2)
		buf = append(buf, ' ')
		for i := 0; i < len(text); i++ {
			buf = append(buf, foldTable[text[i]])
		}
		buf = append(buf, ' ')
		for _, p := range pats {
			if bytes.Contains(buf, p) {
				return true
			}
		}
		return false
	}

	alphabet := []byte{'a', 'h', 'e', 'r', 's', 'i', ' ', 'k', 'y'}
	rng := rand.New(rand.NewSource(20260822))
	for i := 0; i < 20000; i++ {
		n := rng.Intn(24)
		b := make([]byte, n)
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		text := string(b)

		m := newMatcher(patternSets[i%len(patternSets)])
		pats := patternSets[i%len(patternSets)]

		want := false
		for _, p := range pats {
			if bytes.Contains(b, p) {
				want = true
				break
			}
		}
		if got := m.find(b) >= 0; got != want {
			t.Fatalf("find(%q) presence = %v, naive says %v", text, got, want)
		}
		if got, wantF := m.findFolded(text), foldedContains(text, pats); got != wantF {
			t.Fatalf("findFolded(%q) = %v, folded-naive says %v", text, got, wantF)
		}
	}
}
