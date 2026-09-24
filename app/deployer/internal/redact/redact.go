// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package redact

import "regexp"

const mask = "***"

var rules = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+@`), "${1}" + mask + "@"},
	{regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(\S+\s+)?\S+`), "${1}" + mask},
	{regexp.MustCompile(`(?i)(bearer\s+)\S+`), "${1}" + mask},
	{regexp.MustCompile(`(?i)([\w.-]*(?:pass(?:word)?|secret|token|key|dsn)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|\S+)`), "${1}" + mask},
}

func Line(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

func Lines(lines []string) []string {
	for i := range lines {
		lines[i] = Line(lines[i])
	}
	return lines
}
