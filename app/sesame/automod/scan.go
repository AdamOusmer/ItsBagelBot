// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	// emoji counts emoji pictographs and emoji-structure glue (ZWJ, VS16) in
	// parallel to symbols, driving the emoji-hype rescue in heuristicVerdict.
	// It is deliberately ADDITIVE - pictographs still count in symbols too -
	// because removing them from the symbol numerator would cap symbolRatio at
	// 0.5 whenever emoji dominate, making the >=0.6 threshold and therefore the
	// rescue's "only flag is symbol" precondition arithmetically unreachable.
	emoji int
	// spaces backs the non-space denominator of emojiDominant.
	spaces int
	// lettersNonASCII counts letters above ascii - emoji and symbols are NOT
	// letters, so an english line full of emoji stays at 0 while a genuinely
	// foreign-language line dominates. Gates the (comparatively expensive)
	// language-detection juror.
	lettersNonASCII int
	hasNonASCII     bool
}

// emojiMajority is the fraction of a line's non-space runes that must be emoji
// for the emoji-hype rescue to apply. Same half-line rule as emoteMajority so
// both rescues share one intuition ("half the line decides what it is"); pure
// hype lines measure ~100% emoji, so anything at or below 0.5 catches them
// while ordinary text with decorative emoji (measured well under 20%) stays
// out. Raise it and "😂😂😂 hype!!!" shapes (just over half) start deleting
// again; lower it and two-emoji sign-offs get suppressed alongside real
// symbol-spam.
const emojiMajority = 0.5

// isEmojiRune reports whether r belongs to an emoji composition: the SMP
// pictograph blocks (U+1F000-U+1FAFF: smileys, gestures, skin tones, regional
// indicators), the BMP symbol blocks (U+2600-U+27BF: weather, zodiac,
// dingbats), or the composition glue (U+200D ZWJ, U+FE0F VS16) that fuses
// pieces into one glyph. Hex literals per house style - no emoji in source.
//
// Rejected: unicode/utf8 category-table probes and the golang.org/x/text or
// third-party emoji packages - a per-rune table walk (or a new dependency in a
// leaf package) to decide membership the spec pins to two range compares in a
// loop whose contract is "effectively free". Edges are semantic, not tuned:
// U+1FB00+ (legacy computing) and U+2190-U+25FF (arrows, geometric shapes)
// must stay OUT, because arrow/box-drawing spam is precisely the symbol-noise
// shape the symbol heuristic exists to catch.
func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1f000 && r <= 0x1faff:
		return true
	case r >= 0x2600 && r <= 0x27bf:
		return true
	case r == 0x200d || r == 0xfe0f: // ZWJ, VS16: composition glue
		return true
	}
	return false
}

// emojiDominant reports whether at least half of the line's non-space runes are
// emoji (pictographs or composition glue). Float math matches emoteMajority;
// this only runs on the already-flagged deep path, never the clean bail.
func (s signals) emojiDominant() bool {
	nonSpace := s.runes - s.spaces
	if nonSpace <= 0 {
		return false
	}
	return float64(s.emoji) >= emojiMajority*float64(nonSpace)
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

// classify buckets one rune into the style counters. Split from scan's walk
// so the loop keeps only the repeat-run tracking and each half stays under
// the repo's cyclomatic gate. last is the previous rune (zero on the first),
// which the combining-mark case reads to tell a stacked mark from a base one.
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
	// Emoji-structure glue must not read as evasion: U+200D ZWJ and
	// U+FE0F VS16 are the JOINERS of 👨‍👩‍👧 / 🏳️‍🌈, and counting them
	// as invisible (IsInvisible lists ZWJ) deleted every composed emoji.
	// They route here instead of into zeroWidth; every other invisible
	// (U+200B ZWSP, U+200C ZWNJ, word joiner, BOM, RTL overrides) still
	// counts. ZWJ also does NOT enter symbols - it was never a symbol
	// before, and minting one per joiner would inflate symbolRatio for
	// multi-member families without any evasion being present.
	case isEmojiRune(r):
		s.emoji++
		if r != 0x200d { // ZWJ
			s.symbols++
		}
	case moderation.IsInvisible(r):
		s.zeroWidth++
	// Combining marks ride their base letter and are style-neutral:
	// Devanagari matras, Arabic harakat, NFD accents. The old catch-all
	// counted every one as a symbol, so mark-heavy scripts ran at 3-4x an
	// ASCII line's symbolRatio (measured 2026-08-30: Hindi 0.36, Tamil
	// 0.39, Arabic+harakat 0.42 against symbolRatioHi 0.6) - margin, not
	// evasion. Only a STACKED mark (2nd+ Mn in a row, the zalgo shape)
	// still counts: no natural script stacks marks the way zalgo does
	// (Vietnamese NFD tops out at 2 per letter), so mark-wall spam keeps
	// its symbol signal instead of gaining a free evasion channel.
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
