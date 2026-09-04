// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/validate"
)

// capChatText is the runtime backstop for the response shape CommandResponse
// enforces at save time (at most MaxResponseLines lines, each at most
// MaxResponseLineLength): old stored configs written before the validator, and
// any module path that renders unvalidated third-party values, must still
// publish a bounded message instead of a guaranteed Twitch 400. Untouched
// output comes back byte-identical — the common case pays one split-scan and
// no rebuild — and the cut backs up to a rune start so outgress never marshals
// invalid UTF-8. Only plain chat is capped: announce/pin carry their own,
// smaller Twitch limits handled downstream.
func capChatText(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	changed := false
	if len(lines) > validate.MaxResponseLines {
		lines = lines[:validate.MaxResponseLines]
		changed = true
	}
	for i, line := range lines {
		if len(line) > validate.MaxResponseLineLength {
			lines[i] = truncateLine(line, validate.MaxResponseLineLength)
			changed = true
		}
	}
	if !changed {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

// capEmitText applies the chat cap to one output on the emit sink. A no-op
// for every non-chat carrier.
func capEmitText(o *module.Output) {
	if o.Type != outgress.TypeChat {
		return
	}
	if capped, changed := capChatText(o.Text); changed {
		o.Text = capped
	}
}
