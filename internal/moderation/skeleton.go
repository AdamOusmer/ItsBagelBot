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
// measured before/after in BenchmarkNormalize's comment. Both paths share ONE
// token folding core: the fast path folds raw bytes in place on dst, the
// unicode path buffers each token as lowercased, already-unconditionally-
// folded UTF-8 and runs the SAME byte-level routine on those bytes before
// appending - so there is exactly one quorum counter (tokenQuorum) and one
// gated fold (tokenMark.fold). Both paths process each whitespace-delimited TOKEN
// as a unit (count the quorum once, then fold), never re-walking a token per
// foldable rune; writeSkelRune stages the unicode walk's runes;
// skelKindOf classifies them.
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
		if !printableASCII(text[i]) {
			return false
		}
	}
	return true
}

// normalizeASCII is the fast path: raw bytes land on dst while the scan walks
// a token, and each finished whitespace-delimited token is then folded IN
// PLACE on dst by the shared tokenMark fold core - one forward quorum count, one
// forward fold, no re-walk per foldable rune and no scratch buffer beyond
// caller-owned dst itself, so an ordinary chat line normalizes alloc-free
// (measured in BenchmarkNormalize's comment). Splits on every isSkelSpace byte
// rather than ' ' alone: Normalize only routes plain-ascii input here (where
// the extra bytes cannot occur), and MatchFloorPrescan reuses this exact core
// to project its virtual skeleton, keeping both scans' token semantics one
// definition.
func normalizeASCII(dst []byte, text string) []byte {
	tok := tokenMark{}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if isSkelSpace(c) {
			dst = tok.fold(dst)
			if !tok.spaced {
				dst = append(dst, ' ')
			}
			tok.restart(len(dst))
			continue
		}
		tok.spaced = false
		dst = append(dst, c)
	}
	return tok.fold(dst)
}

// tokenMark is the in-flight token inside a skeleton buffer: the concept
// "(offset awaiting its fold, whitespace-collapse state)" as one named value.
// fold is THE token folding core both Normalize paths run: it lowers and
// confusable-folds dst[at:] in place. Lookalike letters fold unconditionally;
// leet digits/symbols fold only when tokenQuorum held for the finished token.
// A digit never counts toward its own quorum because it can never vote at
// all, so "1337" cannot vote itself into "leet".
type tokenMark struct {
	at     int
	spaced bool
}

func (t *tokenMark) restart(at int) { t.at = at; t.spaced = true }

func (t *tokenMark) fold(dst []byte) []byte {
	foldTokenBytes(dst[t.at:])
	return dst
}

// foldTokenBytes lowers and confusable-folds one finished token in place.
func foldTokenBytes(tok []byte) {
	leet := tokenQuorum(tok)
	for i, c := range tok {
		tok[i] = foldByte(c, leet)
	}
}

// foldByte lowercases one skeleton byte and applies its confusable fold.
// Leet digits live in their own gated table: they fold only when the caller
// established the token's two-letter quorum, everyone else folds always.
func foldByte(c byte, leet bool) byte {
	c = asciiLower(c)
	if to, gated := leetFolds[c]; gated {
		if leet {
			return to
		}
		return c
	}
	if to, ok := confusables[rune(c)]; ok {
		return byte(to)
	}
	return c
}

// tokenQuorum reports whether tok carries >=2 real ascii letter BYTES - the
// quorum that lets leet digits fold. One forward pass with an early exit
// bounding the scan; more letters change nothing. The unicode path satisfies
// this byte-level test without losing votes to multi-byte runes precisely
// because writeSkelRune lands every lookalike LETTER in the buffer as its
// single-byte latin fold (multi-byte UTF-8 never contains an ascii-range
// byte), so a lookalike votes exactly once like the fast path's raw letters;
// leet digits never vote, not even each other, so "1080" stays "1080".
func tokenQuorum(tok []byte) bool {
	votes := 0
	for _, c := range tok {
		if asciiLower(c) >= 'a' && asciiLower(c) <= 'z' {
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

// writeSkelRune stages one LOWERCASED post-NFKC rune onto the unicode path's
// token buffer as UTF-8: lookalike LETTERS fold unconditionally here - their
// single-byte latin fold is what votes toward the shared tokenQuorum and what
// a quorum-less token must still emit - while leet digits/symbols land raw
// and wait for the quorum-gated fold in the shared tokenMark fold core. Lowercase
// first, THEN stage (and fold): a single lowercase confusables entry catches
// an uppercase cross-script lookalike too (uppercase Cyrillic 'А' lowercases
// to 'а' before the fold), closing an evasion gap.
func writeSkelRune(tok []byte, lr rune) []byte {
	if lr < utf8.RuneSelf {
		if _, gated := leetFolds[byte(lr)]; gated {
			return utf8.AppendRune(tok, lr)
		}
	}
	if f, ok := confusables[lr]; ok {
		lr = f
	}
	return utf8.AppendRune(tok, lr)
}

// tokenBuf pools the reusable byte buffer normalizeUnicode accumulates each
// whitespace-token into before flushing it folded - the unicode twin of the
// fast path's reuse of caller-owned dst, keeping the deep path's per-token
// bookkeeping allocation-free at chat-line scale. Growth past the initial cap
// happens only on absurdly long tokens.
var tokenBuf = sync.Pool{New: func() any { b := make([]byte, 0, 64); return &b }}

// flushUnicodeToken runs the buffered token through the SAME byte-level
// tokenMark fold core the fast path uses and appends it to dst, emptying the
// buffer. The buffer already holds lowercased, unconditionally-folded UTF-8
// (writeSkelRune), so folding's only remaining work here is the quorum-
// gated leet fold; multi-byte runes pass its byte loop untouched because no
// confusables key falls in a UTF-8 continuation or lead-byte position.
func flushUnicodeToken(dst []byte, tbp *[]byte) []byte {
	tok := *tbp
	if len(tok) == 0 {
		return dst
	}
	foldTokenBytes(tok)
	dst = append(dst, tok...)
	*tbp = tok[:0]
	return dst
}

// normalizeUnicode is the rune-aware path over sanitized input: NFKC
// compatibility folding, strip invisible/control/combining runes, per-rune
// confusable fold, collapse whitespace runs. Kept runes stage into the pooled
// byte buffer via writeSkelRune and flush as a finished unit at every token
// boundary.
func normalizeUnicode(dst []byte, text string) []byte {
	nf := norm.NFKC.AppendString(nil, sanitizeUTF8(text))
	tbp := tokenBuf.Get().(*[]byte)
	defer tokenBuf.Put(tbp)
	spaced := false
	for i := 0; i < len(nf); {
		r, size := utf8.DecodeRune(nf[i:])
		i += size
		switch skelKindOf(r) {
		case skelStrip:
			continue
		case skelSpace:
			dst = flushUnicodeToken(dst, tbp)
			if !spaced {
				dst = append(dst, ' ')
			}
			spaced = true
			continue
		}
		spaced = false
		*tbp = writeSkelRune(*tbp, unicode.ToLower(r))
	}
	return flushUnicodeToken(dst, tbp)
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
