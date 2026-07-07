package moderation

import "testing"

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
