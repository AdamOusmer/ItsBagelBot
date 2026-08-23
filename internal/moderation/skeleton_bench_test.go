// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import "testing"

// Bench corpus mirrors real chat: a short ascii line, a leet-heavy line, and
// a non-ascii (Cyrillic-evasion) line that must take the NFKC path.
//
// Measured (Apple M1 Pro, go1.26.6), before -> after the token-as-a-unit fold
// redesign (2026-08-23, round-2 code-health: the per-gated-rune token re-walk
// and its backward tokStart bookkeeping were deleted outright):
//
//	ascii    332-346 ns/op   0 B/op  0 allocs  ->  404-443 ns/op   0 B/op  0 allocs
//	leet     296-298 ns/op   0 B/op  0 allocs  ->  344-377 ns/op   0 B/op  0 allocs
//	nonascii 1074-1079 ns/op 120 B/op 4 allocs -> 1346-1416 ns/op 120 B/op 4 allocs
//
// The pure-ascii fast path keeps its zero-alloc pin (folding happens IN PLACE
// on caller-owned dst); the non-ascii path keeps its NFKC allocation
// (unavoidable - that is the normalization) and pays ~25% for the pooled rune
// buffer plus its flush pass - deep-path-only cost, accepted to make counting
// and folding single linear passes per token.
var benchLines = []struct {
	name string
	text string
}{
	{"ascii", "yo lets gooo the new patch is actually insane tonight boys"},
	{"leet", "fr33 n1tr0 @ grabify.link claim your pr1ze now!!"},
	{"nonascii", "please visit gr" + string(rune(0x0410)) + "bify.link for the reward soon"},
}

func BenchmarkNormalize(b *testing.B) {
	buf := make([]byte, 0, 256)
	for _, bb := range benchLines {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf = Normalize(buf[:0], bb.text)
			}
		})
	}
}
