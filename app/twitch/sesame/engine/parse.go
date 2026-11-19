// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "strings"

type commandInput struct {
	name string
	args string
}

func parseCommand(text string) (commandInput, bool) {
	trimmed := strings.TrimLeft(text, " ")
	if !strings.HasPrefix(trimmed, "!") {
		return commandInput{}, false
	}
	body := strings.TrimPrefix(trimmed, "!")
	name, args, _ := strings.Cut(body, " ")
	if name == "" {
		return commandInput{}, false
	}
	return commandInput{name: name, args: strings.TrimSpace(args)}, true
}

func splitTrailingDigits(s string) (base, digits string) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	return s[:i], s[i:]
}
