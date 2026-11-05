// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

// TruncateRunes keeps at most limit runes of s.
func TruncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	count := 0
	for i := range s {
		if count == limit {
			return s[:i]
		}
		count++
	}
	return s
}
