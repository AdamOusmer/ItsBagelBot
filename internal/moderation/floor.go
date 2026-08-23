// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package moderation holds the content primitives shared by everything that
// judges or emits user-authored text: the skeleton normalizer (NFKC +
// confusable fold), the Aho-Corasick matcher, the categorized lexicon artifact,
// and the immovable floor. sesame's inline automod builds its gate on these,
// and the trust-boundary validators (internal/domain/validate) use CheckFloor
// so the commands/modules services reject floor content at save time - the bot
// must never store or emit it regardless of any per-channel setting.
package moderation

import (
	"bytes"
	"strings"
)

// The infrastructure floor: objectively-abusive hosts and bait, safe to keep in
// source (no slur ever sits here; those live in the lexicon artifact). Matched
// word-bounded against the normalized skeleton.
var (
	// IPLoggerDomains are IP-grabber/logger hosts used to dox, swat or DDoS.
	IPLoggerDomains = []string{
		"grabify.link", "iplogger.org", "iplogger.com", "iplogger.ru",
		"2no.co", "yip.su", "blasze.com", "stopify.co", "ps3cfw.com", "ipgrabber",
	}
	// ScamTerms are classic chat-scam bait phrases. They are chat-floor only:
	// CheckFloor (dashboard save-time) deliberately excludes them, because a
	// broadcaster's own giveaway command legitimately says "claim your prize".
	ScamTerms = []string{
		"free bits", "free gift sub", "free nitro", "cheap followers",
		"cheap viewers", "buy followers", "claim your prize",
	}
)

// FloorKind identifies which floor list matched. The zero value is "no hit".
type FloorKind uint8

const (
	FloorNone FloorKind = iota
	FloorIPLogger
	FloorScam
)

// String returns the automod rule name for the kind ("ip_logger", "scam"),
// matching defaultCategories in sesame's automod so verdicts keep their rules.
func (k FloorKind) String() string {
	switch k {
	case FloorIPLogger:
		return "ip_logger"
	case FloorScam:
		return "scam"
	default:
		return "none"
	}
}

// floorHit pairs the matched list's FloorKind with the raw term that matched;
// the zero value is "no hit". It is the internal currency between the shared
// boundary walkers and the three scan entries (MatchFloor, MatchFloorPrescan,
// CheckFloor), so no matcher hands back a bare index or a loose (kind, term)
// tuple each caller has to re-pair with its own list.
type floorHit struct {
	kind FloorKind
	term string
}

// ipLoggerPatterns are the domains compiled into SKELETON space once at init,
// so per-message scans compare like against like. Compiling raw was a real FN:
// "ps3cfw.com" carries '3', a quorum-gated leet digit, so every real spelling
// of it normalizes to "psecfw.com" and a raw-pattern scan could never match
// its own normalized input. Same discipline as newLexicon, which normalizes
// its terms for exactly this reason. Runs at init: allocations here never
// touch a message path.
var ipLoggerPatterns = func() [][]byte {
	out := make([][]byte, len(IPLoggerDomains))
	for i, d := range IPLoggerDomains {
		out[i] = Normalize(nil, d)
	}
	return out
}()

// scamPhrases are the ScamTerms split into their word tokens and normalized
// into skeleton space, compiled once for the adjacent-token walk below. All
// current words are pure letters (identity under Normalize); the pass stays so
// a future term carrying digits cannot reintroduce the raw-vs-skeleton drift
// the domain patterns above carried.
var scamPhrases = func() [][][]byte {
	out := make([][][]byte, len(ScamTerms))
	for i, t := range ScamTerms {
		words := strings.Fields(t)
		seq := make([][]byte, len(words))
		for w, word := range words {
			seq[w] = Normalize(nil, word)
		}
		out[i] = seq
	}
	return out
}()

// isFloorTokenByte reports whether a skeleton byte belongs to a floor-matching
// token: lowercase latin letters only. Digits, punctuation, spaces and
// non-latin runes are separators between tokens; the skeleton has already
// folded lookalike letters and qualified leet digits onto this alphabet.
func isFloorTokenByte(b byte) bool { return 'a' <= b && b <= 'z' }

func isFloorAlnumByte(b byte) bool {
	return 'a' <= b && b <= 'z' || '0' <= b && b <= '9'
}

// skelBuf adapts a real normalized skeleton for the shared walkers below:
// MatchFloor reads the deep path's buffer, and MatchFloorPrescan builds its
// projection into the same shape (see the clean-path pre-scan section), so the
// walkers need exactly one instantiation.
type skelBuf []byte

func (b skelBuf) len() int           { return len(b) }
func (b skelBuf) at(i int) byte      { return b[i] }
func (b skelBuf) isToken(i int) bool { return isFloorTokenByte(b[i]) }

func (b skelBuf) index(d []byte, from int) int {
	j := bytes.Index(b[from:], d)
	if j < 0 {
		return -1
	}
	return from + j
}

// MatchFloor reports whether a normalized skeleton carries the chat-floor
// infrastructure blocklists, without allocating (the automod deep path runs it
// on every flagged message). Matching is deliberately word-bounded rather than
// substring:
//
//   - IP-logger domains hit only at host-label boundaries: preceded by start,
//     a space, a dot or a scheme slash; followed by end, a space, a dot, a
//     slash or a port colon. Every real shape hits (bare, www./subdomains,
//     https://, :port, trailing path), while "notgrabify.link" and
//     "grabify.links" stay clean - naive substring matching timed people out
//     over hosts that do not exist.
//   - Scam phrases hit only as ADJACENT whole tokens, with any punctuation run
//     acting as a separator: "free nitro now", "FREE-NITRO!!" and "free,nitro"
//     all hit, while "free nitrogen" ([free][nitrogen]) stays clean - raw
//     Contains flagged benign chemistry talk with a 600s timeout. Known false
//     negative, accepted: one fused token ("freenitro") splits no tokens and
//     therefore misses; splitting fused words needs edit-distance machinery
//     whose own false-positive rate is not worth 600s timeouts.
//
// Returns the kind plus the offending term. skel must be Normalize output.
func MatchFloor(skel []byte) (FloorKind, string) {
	if len(skel) == 0 {
		return FloorNone, ""
	}
	v := skelBuf(skel)
	if h := matchDomain(v); h.kind != FloorNone {
		return h.kind, h.term
	}
	if h := matchScam(v); h.kind != FloorNone {
		return h.kind, h.term
	}
	return FloorNone, ""
}

// matchDomain returns the first IP-logger domain occurring at host-label
// boundaries in v, or the zero hit. Zero allocations: bytes.Index scans,
// boundaries inspect neighbors in place.
func matchDomain(v skelBuf) floorHit {
	for di, d := range ipLoggerPatterns {
		for i := 0; ; {
			s := v.index(d, i)
			if s < 0 {
				break
			}
			if domainBounded(v, s, s+len(d)) {
				return floorHit{FloorIPLogger, IPLoggerDomains[di]}
			}
			i = s + 1 // overlapping occurrences cannot matter, but stay correct
		}
	}
	return floorHit{}
}

// domainBounded reports whether an occurrence at [s,e) sits at host-label
// boundaries. Alphanumeric neighbors mean the occurrence is glued into a
// longer word ("notgrabify.link" left, "grabify.links" right) and is either a
// different host or benign prose - both released on purpose; every separator
// neighbor (space, dot, slash, colon, quotes, brackets) keeps the hit.
func domainBounded(v skelBuf, s, e int) bool {
	if s > 0 && isFloorAlnumByte(v.at(s-1)) {
		return false
	}
	return e == v.len() || !isFloorAlnumByte(v.at(e))
}

// matchScam returns the first ScamTerms phrase present in v as an adjacent
// token sequence, or the zero hit. One pass, manual token walking, zero
// allocations.
func matchScam(v skelBuf) floorHit {
	for start, end, ok := nextFloorToken(v, 0); ok; start, end, ok = nextFloorToken(v, end) {
		for pi := range scamPhrases {
			if phraseAt(v, start, end, scamPhrases[pi]) {
				return floorHit{FloorScam, ScamTerms[pi]}
			}
		}
	}
	return floorHit{}
}

// nextFloorToken advances from past any separator run and returns the bounds
// of the next [a-z]-token, ok=false when the view ends first. This is THE
// shared token-boundary matcher: both the authoritative scan's phrase walk and
// the pre-scan's consume tokens through it (matchScam here, phraseAt below),
// so word-bounded semantics are one definition instead of two hand-synced
// copies that can drift.
func nextFloorToken(v skelBuf, from int) (start, end int, ok bool) {
	i := from
	n := v.len()
	for i < n && !v.isToken(i) {
		i++
	}
	if i >= n {
		return 0, 0, false
	}
	start = i
	for i < n && v.isToken(i) {
		i++
	}
	return start, i, true
}

// phraseAt reports whether the phrase words match tokens starting at
// [start,end) and continuing across any separator run (space, punctuation).
func phraseAt(v skelBuf, start, end int, words [][]byte) bool {
	for wi, w := range words {
		if wi > 0 {
			var ok bool
			start, end, ok = nextFloorToken(v, end)
			if !ok {
				return false
			}
		}
		if end-start != len(w) || !eqRun(v, start, w) {
			return false
		}
	}
	return true
}

// eqRun compares w against the view bytes at [pos,pos+len(w)).
func eqRun(v skelBuf, pos int, w []byte) bool {
	for k, wb := range w {
		if v.at(pos+k) != wb {
			return false
		}
	}
	return true
}

// ---- clean-path pre-scan ---------------------------------------------------
//
// The automod's clean path (short, plain, unflagged line) exists to stay
// allocation-free, so it cannot run full Normalize to call MatchFloor itself -
// without the pre-scan below, a bare short "grabify.link" or "get free nitro"
// bailed clean and skipped the immovable floor entirely. The pre-scan instead
// projects raw text onto its ASCII-skeleton bytes by running normalizeASCII's
// exact core (lowercase, quorum-gated leet fold, separators collapsed) into a
// small caller-shaped stack buffer - no NFKC pass, no allocation on the pinned
// path - and then runs the DEEP SCAN'S OWN walkers over those bytes. Parity
// with the authoritative scan is therefore structural, not incidental: same
// patterns (already skeleton-space), same boundaries, same token rules, one
// instantiation.
//
// A hit is a ROUTING decision only: the caller takes the deep path, rebuilds
// the real skeleton and lets floorInfra's authoritative MatchFloor decide, so
// even an unexpected pre-scan hit costs one deep trip on a <=40-char line and
// nothing more. Best-effort by contract, and one-directional BOTH ways
// (tightened 2026-08-22 after FuzzMatchFloor caught "0\x00grA81fY.l1nk" hitting
// the pre-scan while the authoritative scan released its skeleton
// "ograbify.link" - the control byte vanishes under Normalize, gluing the
// leading folded digit onto the host label):
//
//   - it may RELEASE shapes MatchFloor catches (non-ascii obfuscation);
//   - it may OVER-ROUTE shapes MatchFloor releases (strippable bytes glue
//     skeleton tokens that raw-text boundaries still split).
//
// Both directions are safe because a hit is ROUTING only: callers rebuild the
// skeleton and let MatchFloor decide authoritatively, so an over-route costs
// one deep trip on a short line and can never mint a verdict by itself.
// Non-ascii input never reaches here from the gate (the bail requires pure
// ascii); for other callers non-ascii bytes read as separators, which can only
// miss, never fabricate.

// MatchFloorPrescan reports which chat-floor infrastructure list raw text
// carries, without normalizing or allocating. See the pre-scan section above
// for the routing-only contract.
func MatchFloorPrescan(text string) (FloorKind, string) {
	var buf [64]byte // gate lines are <= shortLen(40); growth past cap allocates once
	v := skelBuf(normalizeASCII(buf[:0], text))
	h := matchDomain(v)
	if h.kind == FloorNone {
		h = matchScam(v)
	}
	return h.kind, h.term
}

// Terms returns the raw terms loaded for a category (rule reporting, tests).
func (l *Lexicon) Terms(c Category) []string {
	if l == nil {
		return nil
	}
	return l.terms[c]
}

// CheckFloor reports whether text carries immovable-floor content: an identity
// slur (hate lexicon, word-bounded over the skeleton so leet and lookalike
// obfuscation folds onto it) or an IP-logger/grabber host. This is the save-time
// gate for dashboard-authored text (custom commands, module templates): the bot
// posts that text as itself, so hosting hate or dox infrastructure there risks
// the broadcaster's channel AND the bot account platform-wide. Everything
// milder (profanity, scam-sounding phrasing) is deliberately allowed - people
// say what they want, the floor is only hate and abuse infrastructure.
//
// Domain matching shares MatchFloor's label-boundary semantics, which releases
// only hosts that were never real ("notgrabify.link", "grabify.links") - every
// genuine URL shape still saves-time-rejects exactly as before. ScamTerms stay
// excluded here: giveaway commands legitimately say "claim your prize".
//
// Returns the offending term and true on a hit.
func CheckFloor(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	skel := Normalize(nil, text)
	if len(skel) == 0 {
		return "", false
	}

	if h := matchDomain(skelBuf(skel)); h.kind != FloorNone {
		return h.term, true
	}

	padded := make([]byte, 0, len(skel)+2)
	padded = append(padded, ' ')
	padded = append(padded, skel...)
	padded = append(padded, ' ')
	if cat, term := EmbeddedLexicon().Scan(padded, true); cat == CatHate {
		return term, true
	}
	return "", false
}
