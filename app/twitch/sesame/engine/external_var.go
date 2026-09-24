// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "unicode/utf8"

const MaxExternalVarBytes = 100

func ExternalVar(v string) string {
	return truncateLine(sanitizeVar(v), MaxExternalVarBytes)
}

func truncateLine(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
