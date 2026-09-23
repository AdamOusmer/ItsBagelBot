// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package redact scrubs credentials out of log lines before they become run
// evidence. A Failure's LogTail is persisted in the DEPLOY_RUNS KV (active
// run plus the last 50), published on bagel.deploy.events.<id> and rendered
// in the console, so anything it carries outlives the pod that printed it.
//
// The most common crash-loop shape is a service printing its DSN or a token
// while failing to connect on startup. Redacting by exact value was
// rejected: the deployer has no secrets RBAC by design, so it cannot know
// the values. These patterns trade a few false positives (a harmless
// "key=" field loses its value) for never storing the credential.
package redact

import "regexp"

const mask = "***"

var rules = []struct {
	re   *regexp.Regexp
	repl string
}{
	// scheme://user:pass@host keeps the scheme and host so the line still
	// says which endpoint failed.
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+@`), "${1}" + mask + "@"},
	// Authorization headers and bare bearer tokens.
	{regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(\S+\s+)?\S+`), "${1}" + mask},
	{regexp.MustCompile(`(?i)(bearer\s+)\S+`), "${1}" + mask},
	// key=value and key: value where the key names a secret, including
	// compound keys such as DB_PASSWORD, api_key or clientSecret.
	{regexp.MustCompile(`(?i)([\w.-]*(?:pass(?:word)?|secret|token|key|dsn)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|\S+)`), "${1}" + mask},
}

// Line returns s with every credential-shaped span masked.
func Line(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// Lines masks every line in place and returns the slice, so a caller can
// wrap an assignment without a temporary.
func Lines(lines []string) []string {
	for i := range lines {
		lines[i] = Line(lines[i])
	}
	return lines
}
