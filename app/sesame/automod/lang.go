package automod

import (
	"unicode"

	"github.com/abadojack/whatlanggo"
)

// The language juror protects international chats from false lexicon hits. The
// confusable fold maps Cyrillic/Greek lookalikes onto latin, which is exactly
// right for catching obfuscation ("grаbify") but means a genuine Russian or
// Greek sentence partially folds into latin soup that can contain an English
// lexicon term by accident. When the dominant script of the RAW text is
// reliably non-latin, the gate restricts the lexicon scan to the floor
// categories (ascii infrastructure: domains, scam bait) and skips the
// word-list categories entirely: a channel chatting in another language is
// never judged by an English word list.
//
// whatlanggo is pure Go with its trigram profiles compiled in - no model file,
// no cgo, microseconds per call - and runs only on the deep (already flagged
// or non-ascii) path.
func isNonLatin(text string) bool {
	info := whatlanggo.Detect(text)
	return info.Script != nil && info.Script != unicode.Latin
}
