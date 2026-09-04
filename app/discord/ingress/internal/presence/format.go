// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package presence

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// printer formats counts with locale thousands separators (1,234 rather than
// 1234). English is fixed rather than following any per-user locale: this is
// a bot-wide status line with no single viewer to localize for.
var printer = message.NewPrinter(language.English)

// activityName renders the status line's name half. It must NOT contain the
// word "Watching": Discord's client prepends the activity type's own label
// (Activity Type 3), so including it here would render as "Watching Watching
// N streams". See gateway.presenceUpdateBody for the type value and the docs
// citation.
func activityName(total int) string {
	return printer.Sprintf("%d %s", total, streamsWord(total))
}

func streamsWord(total int) string {
	if total == 1 {
		return "stream"
	}
	return "streams"
}
