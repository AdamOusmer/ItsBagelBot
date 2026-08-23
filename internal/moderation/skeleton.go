// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// IsInvisible reports the explicit zero-width and RTL/LTR-override code points
// spam uses to break up tokens or defeat dedup. Counted as a signal by scan.
// Written as hex code points so no invisible rune ever sits in the source.
func IsInvisible(r rune) bool {
	switch r {
	case 0x200b, // zero width space
		0x200c, // zero width non-joiner
		0x200d, // zero width joiner
		0x2060, // word joiner
		0xfeff, // zero width no-break space / BOM
		0x180e, // Mongolian vowel separator
		0x202e, // right-to-left override
		0x202d: // left-to-right override
		return true
	}
	return false
}

// isStrippable reports code points removed from the skeleton: the invisible set
// above, all format (Cf) and non-spacing-mark (Mn, Zalgo) runes, and controls.
func isStrippable(r rune) bool {
	return IsInvisible(r) || unicode.IsControl(r) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r)
}

// confusables folds common cross-script and leet lookalikes to their latin
// skeleton. Keys are the LOWERCASE code point: Normalize lowercases before it
// folds, so a single lowercase entry catches both cases (an uppercase Cyrillic 'А'
// lowercases to 'а' and then folds), and the map stays half the size. A curated
// set (Cyrillic, Greek, math/fullwidth left to NFKC, digit/symbol leet); the full
// Unicode confusables table is loaded from the pattern artifact in a later phase.
var confusables = map[rune]rune{
	// Cyrillic lowercase lookalikes.
	0x0430: 'a', 0x0435: 'e', 0x043e: 'o', 0x0440: 'p', 0x0441: 'c', // а е о р с
	0x0445: 'x', 0x0443: 'y', 0x043a: 'k', 0x043c: 'm', 0x0442: 't', // х у к м т
	0x043d: 'h', 0x0432: 'b', 0x0456: 'i', 0x0455: 's', 0x0458: 'j', // н в і ѕ ј
	0x0501: 'd', 0x04bb: 'h', 0x0433: 'r', // ԁ һ г
	// Greek lowercase lookalikes.
	0x03b1: 'a', 0x03b2: 'b', 0x03b5: 'e', 0x03b7: 'h', 0x03b9: 'i', // α β ε η ι
	0x03ba: 'k', 0x03bd: 'v', 0x03bf: 'o', 0x03c1: 'p', 0x03c4: 't', // κ ν ο ρ τ
	0x03c5: 'y', 0x03c7: 'x', 0x03b6: 'z', 0x03c9: 'w', 0x03c3: 'o', // υ χ ζ ω σ
	// Digit/symbol leet (scoped to skeleton blocklist matching). GATED - see
	// isLeetFold and the quorum in Normalize.
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b', '@': 'a', '$': 's',
}

// isLeetFold reports whether a confusable key is a digit/symbol whose fold is
// gated behind the two-letter quorum in Normalize. Unconditional folding turned
// any number into letter soup ("1080" -> "ioao", "1337" -> "ieet") that could
// drift onto lexicon terms or wreck dedup fingerprints; letters-only obfuscation
// ("h4te", "n0t") carries its own real letters and keeps folding.
func isLeetFold(r rune) bool {
	switch r {
	case '0', '1', '3', '4', '5', '7', '8', '@', '$':
		return true
	}
	return false
}

// Normalize folds a message into its detection skeleton and writes it into dst (a
// pooled buffer), returning the slice: NFKC (fullwidth/compat to ascii), strip
// invisible/control/combining, fold confusable lookalikes to latin, lowercase,
// collapse whitespace runs. It runs only on the flagged path, so its allocations
// never touch the clean hot path.
//
// Leet digits/symbols ('0','1','3','4','5','7','8','@','$') fold ONLY when
// their containing whitespace-token carries >=2 real ascii letters (lookalike
// letters count): "h4te"/"s3xual"/"n0t" still fold, "1080"/"1337"/"<3" keep
// their digits. Lowercase-before-fold ordering preserved.
//
// Pure-ascii input takes a byte-wise fast path that skips NFKC and rune
// decoding entirely (NFKC is identity on ASCII; the only strippable ASCII
// runes are C0 controls + DEL): an ordinary chat line normalizes alloc-free,
// measured before/after in BenchmarkNormalize's comment. The transform lives
// in small staged helpers - isPlainASCII routes between normalizeASCII and
// normalizeUnicode; both process each whitespace-delimited TOKEN as a unit
// (count the quorum letters once, then fold), never re-walking a token per
// foldable rune; foldASCIIToken/foldLoweredRune carry the quorum-gated fold;
// skelKindOf classifies the unicode walk's runes.
func Normalize(dst []byte, text string) []byte {
	dst = dst[:0]
	if isPlainASCII(text) {
		return normalizeASCII(dst, text)
	}
	return normalizeUnicode(dst, text)
}

// isPlainASCII reports whether text can take the byte-wise fast path: every
// byte is printable-or-space ascii, on which NFKC is identity and the only
// strippable runes are C0 controls + DEL (both excluded here).
func isPlainASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if c := text[i]; c >= utf8.RuneSelf || c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// isSkelSpace reports whether b is a whitespace byte that survives Normalize
// as a token boundary (the skeleton emits a single ' ' for any run of these).
func isSkelSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\v' || b == '\f' || b == '\r'
}

// normalizeASCII is the fast path: raw bytes land on dst while the scan walks
// a token, and each finished whitespace-delimited token is then folded IN
// PLACE on dst - one forward quorum count, one forward fold, no re-walk per
// foldable rune and no scratch buffer beyond caller-owned dst itself, so an
// ordinary chat line normalizes alloc-free (measured in BenchmarkNormalize's
// comment). Splits on every isSkelSpace byte rather than ' ' alone: Normalize
// only routes plain-ascii input here (where the extra bytes cannot occur), and
// MatchFloorPrescan reuses this exact core to project its virtual skeleton,
// keeping both scans' token semantics one definition.
func normalizeASCII(dst []byte, text string) []byte {
	spaced := false
	mark := 0 // dst offset where the in-flight token began
	for i := 0; i < len(text); i++ {
		c := text[i]
		if isSkelSpace(c) {
			dst = foldASCIIToken(dst, mark)
			if !spaced {
				dst = append(dst, ' ')
			}
			spaced = true
			mark = len(dst)
			continue
		}
		spaced = false
		dst = append(dst, c)
	}
	return foldASCIIToken(dst, mark)
}

// foldASCIIToken lowers and confusable-folds the raw token bytes dst[mark:]
// in place: lookalike letters fold unconditionally; leet digits/symbols fold
// only when asciiQuorum held for the finished token. A digit never counts
// toward its own quorum because it can never vote at all, so "1337" cannot
// vote itself into "leet".
func foldASCIIToken(dst []byte, mark int) []byte {
	tok := dst[mark:]
	leet := asciiQuorum(tok)
	for i, c := range tok {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if f, ok := confusables[rune(c)]; ok && (!isLeetFold(rune(c)) || leet) {
			c = byte(f)
		}
		tok[i] = c
	}
	return dst
}

// asciiQuorum reports whether tok carries >=2 real ascii letters - the quorum
// that lets leet digits fold. One forward pass with an early exit bounding the
// scan; more letters change nothing.
func asciiQuorum(tok []byte) bool {
	votes := 0
	for _, c := range tok {
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' {
			votes++
			if votes >= 2 {
				return true
			}
		}
	}
	return false
}

// skelKind routes one post-NFKC rune through the skeleton walk.
type skelKind uint8

const (
	skelKeep  skelKind = iota // skeleton material: lowercase, fold, emit
	skelStrip                 // strippable: vanishes without ending the token
	skelSpace                 // surviving separator: collapse into a single ' '
)

// skelKindOf classifies r against the strip/space rules of the walk. Stripping
// MUST precede the space test (found by FuzzNormalize, 2026-08-22):
// '\v'/'\f'/NEL are BOTH strippable controls and unicode.IsSpace, and the walk
// strips them - so "A\f0A"'s skeleton is "a0a", ONE token. Counting the \f as
// a token end saw one letter and refused to fold the '0', making pass one emit
// "a0a" whose own re-normalization folds to "aoa": non-idempotent output that
// disagreed with itself between raw and normalized forms. A rune that will not
// exist in the skeleton must never end a token; boundaries are exactly the
// runes that survive as separators.
func skelKindOf(r rune) skelKind {
	switch {
	case isStrippable(r):
		return skelStrip
	case unicode.IsSpace(r):
		return skelSpace
	default:
		return skelKeep
	}
}

// foldLoweredRune applies the confusable fold to one lowercased post-NFKC
// rune: lookalike letters fold unconditionally; leet digits/symbols fold only
// when their containing token cleared the two-letter quorum.
func foldLoweredRune(lr rune, quorum bool) rune {
	if f, ok := confusables[lr]; ok && (!isLeetFold(lr) || quorum) {
		return f
	}
	return lr
}

// tokenQuorum reports whether the LOWERCASED buffered runes carry >=2 quorum
// votes: a rune votes when its unconditionally-folded form is a latin letter.
// Lookalike LETTERS (Cyrillic а, Greek α) vote, so "grаb1fy"-style mixed
// evasion still folds; leet digits never vote, not even each other, so "1080"
// stays "1080". One forward pass with an early exit bounding the scan.
func tokenQuorum(tok []rune) bool {
	votes := 0
	for _, lr := range tok {
		if f := foldLoweredRune(lr, false); 'a' <= f && f <= 'z' {
			votes++
			if votes >= 2 {
				return true
			}
		}
	}
	return false
}

// tokenRunes pools the reusable rune buffer normalizeUnicode accumulates each
// whitespace-token into before flushing it folded - the unicode twin of the
// fast path's reuse of caller-owned dst, keeping the deep path's per-token
// bookkeeping allocation-free at chat-line scale. Growth past the initial cap
// happens only on absurdly long tokens.
var tokenRunes = sync.Pool{New: func() any { r := make([]rune, 0, 64); return &r }}

// flushUnicodeToken folds the buffered token onto dst as a unit and empties
// the buffer: ONE forward pass counts the quorum (tokenQuorum), one writes the
// folded UTF-8. The buffered runes are exactly the surviving-token material
// the old per-gated-rune walk re-decoded from the NFKC buffer on every leet
// byte - counting once per token replaces that repeated scan outright.
func flushUnicodeToken(dst []byte, trp *[]rune) []byte {
	tok := *trp
	if len(tok) == 0 {
		return dst
	}
	leet := tokenQuorum(tok)
	var enc [utf8.UTFMax]byte
	for _, lr := range tok {
		f := foldLoweredRune(lr, leet)
		if f < utf8.RuneSelf {
			// Post-NFKC chat material is overwhelmingly ascii; routing the
			// single-byte case around EncodeRune keeps the unicode path within
			// its measured budget (BenchmarkNormalize's comment).
			dst = append(dst, byte(f))
			continue
		}
		n := utf8.EncodeRune(enc[:], f)
		dst = append(dst, enc[:n]...)
	}
	*trp = tok[:0]
	return dst
}

// normalizeUnicode is the rune-aware path over sanitized input: NFKC
// compatibility folding, strip invisible/control/combining runes, per-rune
// confusable fold, collapse whitespace runs. Kept runes buffer into the pooled
// rune slice and flush as a finished unit at every token boundary.
func normalizeUnicode(dst []byte, text string) []byte {
	nf := norm.NFKC.AppendString(nil, sanitizeUTF8(text))
	trp := tokenRunes.Get().(*[]rune)
	defer tokenRunes.Put(trp)
	spaced := false
	for i := 0; i < len(nf); {
		r, size := utf8.DecodeRune(nf[i:])
		i += size
		switch skelKindOf(r) {
		case skelStrip:
			continue
		case skelSpace:
			dst = flushUnicodeToken(dst, trp)
			if !spaced {
				dst = append(dst, ' ')
			}
			spaced = true
			continue
		}
		spaced = false
		// Lowercase first, THEN fold (foldLoweredRune expects the lowered
		// rune): a single lowercase confusables entry then catches an uppercase
		// cross-script lookalike too (uppercase Cyrillic 'А' lowercases to 'а'
		// before the fold), closing an evasion gap.
		*trp = append(*trp, unicode.ToLower(r))
	}
	return flushUnicodeToken(dst, trp)
}

// sanitizeUTF8 replaces every invalid byte run in s with U+FFFD BEFORE NFKC
// (found by FuzzNormalize, 2026-08-22): AppendString has streaming semantics
// and passes a TRAILING incomplete sequence through raw on the assumption more
// input will complete it - a chat line cut mid-rune kept its tail byte
// unnormalized, so the skeleton disagreed with its own re-normalization
// (non-idempotent) and left compatibility letters like U+02B8 unfolded for the
// word-bounded scans. strings.ToValidUTF8 substitutes FFFD exactly as
// DecodeRune already does for interior invalid bytes; valid input (the common
// case) pays one scan and zero allocations.
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, string(rune(0xfffd)))
}
