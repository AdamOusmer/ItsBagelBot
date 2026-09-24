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
		"ushers":  1,
		"hers":    0,
		"this":    3,
		"nothing": -1,
		"":        -1,
	}
	for text, want := range cases {
		if got := m.find([]byte(text)); got != want {
			t.Fatalf("find(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestMatcherWordBoundedTerms(t *testing.T) {
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

func TestMatcherDifferentialNaive(t *testing.T) {
	patternSets := [][][]byte{
		{[]byte("he"), []byte("she"), []byte("hers"), []byte("his")},
		{[]byte(" ass "), []byte(" kys "), []byte("kill yourself ")},
		{[]byte("free nitro"), []byte("nitro")},
		{[]byte("a")},
		{[]byte("abc"), []byte("abcabc")},
	}

	alphabet := []byte{'a', 'h', 'e', 'r', 's', 'i', ' ', 'k', 'y'}
	rng := rand.New(rand.NewSource(20260822))
	for i := 0; i < 20000; i++ {
		text, pats := randomCorpusLine(rng, alphabet, patternSets, i)
		m := newMatcher(pats)

		assertFindAgreesNaiveContains(t, text, m, pats)
		assertFindFoldedAgreesFoldedContains(t, text, m, pats)
	}
}

func randomCorpusLine(rng *rand.Rand, alphabet []byte, patternSets [][][]byte, i int) (string, [][]byte) {
	b := make([]byte, rng.Intn(24))
	for j := range b {
		b[j] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(b), patternSets[i%len(patternSets)]
}

func assertFindAgreesNaiveContains(t *testing.T, text string, m *matcher, pats [][]byte) {
	t.Helper()
	want := containsAnyPattern([]byte(text), pats)
	if got := m.find([]byte(text)) >= 0; got != want {
		t.Fatalf("find(%q) presence = %v, naive says %v", text, got, want)
	}
}

func assertFindFoldedAgreesFoldedContains(t *testing.T, text string, m *matcher, pats [][]byte) {
	t.Helper()
	if got, wantF := m.findFolded(text), foldedContains(text, pats); got != wantF {
		t.Fatalf("findFolded(%q) = %v, folded-naive says %v", text, got, wantF)
	}
}

func foldedContains(text string, pats [][]byte) bool {
	buf := make([]byte, 0, len(text)+2)
	buf = append(buf, ' ')
	for i := 0; i < len(text); i++ {
		buf = append(buf, foldTable[text[i]])
	}
	buf = append(buf, ' ')
	return containsAnyPattern(buf, pats)
}

func containsAnyPattern(b []byte, pats [][]byte) bool {
	for _, p := range pats {
		if bytes.Contains(b, p) {
			return true
		}
	}
	return false
}
