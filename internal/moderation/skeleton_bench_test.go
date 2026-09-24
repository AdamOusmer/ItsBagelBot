// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import "testing"

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
