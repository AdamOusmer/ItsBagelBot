// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import "unicode/utf8"

// MaxExternalVarBytes caps one third-party value a module expands into a chat
// template ({player}, {tags}, an upstream error message). The bound exists at
// the variable-provider level, before ANY template consumes the value: gossip
// replies are external systems whose fields we do not control, so a hostile or
// broken upstream must not be able to size (or control-char-poison) a bot chat
// line through whatever template references it. 100 bytes covers every real
// player name and tag list head with room to spare.
const MaxExternalVarBytes = 100

// ExternalVar sanitizes and caps one third-party value for template
// expansion: sanitizeVar strips control characters (so no embedded newline can
// mint extra lines and no leading slash run can mint a verb) and truncateLine
// caps the length without splitting a UTF-8 rune. Numeric/enum tokens do not
// need this - only values that originate outside the bot.
func ExternalVar(v string) string {
	return truncateLine(sanitizeVar(v), MaxExternalVarBytes)
}

// truncateLine caps s at max bytes without splitting a UTF-8 rune: when the
// cut would land mid-rune it backs up to the previous rune boundary. A
// multibyte tail loses at most three bytes against the cap.
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
