// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"unicode"

	"github.com/abadojack/whatlanggo"
)

func isNonLatin(text string) bool {
	info := whatlanggo.Detect(text)
	return info.Script != nil && info.Script != unicode.Latin
}
