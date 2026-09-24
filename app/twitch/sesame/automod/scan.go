// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"unicode"

	"ItsBagelBot/internal/moderation"
)

type signals struct {
	runes           int
	letters         int
	upper           int
	symbols         int
	maxRepeat       int
	zeroWidth       int
	emoji           int
	spaces          int
	lettersNonASCII int
	hasNonASCII     bool
}

const (
	emojiMajority = 0.5

	zeroWidthJoiner     = 0x200d
	variationSelector16 = 0xfe0f
)

func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1f000 && r <= 0x1faff:
		return true
	case r >= 0x2600 && r <= 0x27bf:
		return true
	case r == zeroWidthJoiner || r == variationSelector16:
		return true
	}
	return false
}

func (s signals) emojiDominant() bool {
	nonSpace := s.runes - s.spaces
	if nonSpace <= 0 {
		return false
	}
	return float64(s.emoji) >= emojiMajority*float64(nonSpace)
}

func (s signals) foreignLeaning() bool {
	return s.letters > 0 && s.lettersNonASCII*3 >= s.letters
}

func (s signals) capsRatio() float64 {
	if s.letters == 0 {
		return 0
	}
	return float64(s.upper) / float64(s.letters)
}

func (s signals) symbolRatio() float64 {
	if s.runes == 0 {
		return 0
	}
	return float64(s.symbols) / float64(s.runes)
}

func (s *signals) classify(r, last rune) {
	switch {
	case unicode.IsLetter(r):
		s.letters++
		if r > unicode.MaxASCII {
			s.lettersNonASCII++
		}
		if unicode.IsUpper(r) {
			s.upper++
		}
	case isEmojiRune(r):
		s.emoji++
		if r != zeroWidthJoiner {
			s.symbols++
		}
	case moderation.IsInvisible(r):
		s.zeroWidth++
	case unicode.Is(unicode.Mn, r):
		if unicode.Is(unicode.Mn, last) {
			s.symbols++
		}
	case unicode.IsSpace(r):
		s.spaces++
	case !unicode.IsDigit(r):
		s.symbols++
	}
}

func scan(text string) signals {
	var s signals
	var last rune
	var run int
	for _, r := range text {
		s.runes++
		if r > unicode.MaxASCII {
			s.hasNonASCII = true
		}
		s.classify(r, last)
		if r == last {
			run++
			if run > s.maxRepeat {
				s.maxRepeat = run
			}
		} else {
			run = 1
			last = r
		}
	}
	if s.maxRepeat == 0 && s.runes > 0 {
		s.maxRepeat = 1
	}
	return s
}
