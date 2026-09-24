// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/validate"
)

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

func capEmitText(o *module.Output) {
	if o.Type != outgress.TypeChat {
		return
	}
	if capped, changed := capChatText(o.Text); changed {
		o.Text = capped
	}
}
