package automod

import "unicode"

// signals are the cheap, allocation-free measures taken in a single pass over the
// raw message. The clean-path check uses them to bail before the costly skeleton
// normalization ever runs, so a plain chat line never allocates.
type signals struct {
	runes       int
	letters     int
	upper       int
	symbols     int
	maxRepeat   int
	zeroWidth   int
	hasNonASCII bool
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
			if unicode.IsUpper(r) {
				s.upper++
			}
		case isInvisible(r):
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
