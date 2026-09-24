// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "strings"

func parseCommand(text string) (name, args string, ok bool) {
	trimmed := strings.TrimLeft(text, " ")
	if !strings.HasPrefix(trimmed, "!") {
		return "", "", false
	}
	body := strings.TrimPrefix(trimmed, "!")
	name, args, _ = strings.Cut(body, " ")
	if name == "" {
		return "", "", false
	}
	return strings.ToLower(name), strings.TrimSpace(args), true
}

func splitTrailingDigits(s string) (base, digits string) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	return s[:i], s[i:]
}
