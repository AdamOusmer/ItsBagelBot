// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package presence

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var printer = message.NewPrinter(language.English)

func activityName(total int) string {
	return printer.Sprintf("%d %s", total, streamsWord(total))
}

func streamsWord(total int) string {
	if total == 1 {
		return "stream"
	}
	return "streams"
}
