package automod

import (
	"unicode"

	"ItsBagelBot/internal/moderation"
)

// signals are the cheap, allocation-free measures taken in a single pass over the
// raw message. The clean-path check uses them to bail before the costly skeleton
// normalization ever runs, so a plain chat line never allocates.
type signals struct {
	runes     int
	letters   int
	upper     int
	symbols   int
	maxRepeat int
	zeroWidth int
	// lettersNonASCII counts letters above ascii - emoji and symbols are NOT
	// letters, so an english line full of emoji stays at 0 while a genuinely
	// foreign-language line dominates. Gates the (comparatively expensive)
	// language-detection juror.
	lettersNonASCII int
	hasNonASCII     bool
}

// foreignLeaning reports whether non-ascii letters make up enough of the line
// (a third or more) to be worth asking the language juror. An obfuscated latin
// line with one Cyrillic lookalike stays below this and is scanned in full;
// emoji do not count at all.
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

// scan walks the raw text once (range over a string does not allocate) and
// gathers the cheap signals.
func scan(text string) signals {
	var s signals
	var last rune
	var run int
	for _, r := range text {
		s.runes++
		if r > unicode.MaxASCII {
			s.hasNonASCII = true
		}
		switch {
		case unicode.IsLetter(r):
			s.letters++
			if r > unicode.MaxASCII {
				s.lettersNonASCII++
			}
			if unicode.IsUpper(r) {
				s.upper++
			}
		case moderation.IsInvisible(r):
			s.zeroWidth++
		case !unicode.IsSpace(r) && !unicode.IsDigit(r):
			s.symbols++
		}
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
