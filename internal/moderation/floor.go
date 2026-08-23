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
	if di := matchDomain(skel); di >= 0 {
		return FloorIPLogger, IPLoggerDomains[di]
	}
	if si := matchScam(skel); si >= 0 {
		return FloorScam, ScamTerms[si]
	}
	return FloorNone, ""
}

// matchDomain returns the index of the first IP-logger domain occurring at
// host-label boundaries in skel, or -1. Zero allocations: bytes.Index scans,
// boundaries inspect neighbors in place.
func matchDomain(skel []byte) int {
	for di, d := range ipLoggerPatterns {
		for i := 0; ; {
			j := bytes.Index(skel[i:], d)
			if j < 0 {
				break
			}
			s := i + j
			if domainBounded(skel, s, s+len(d)) {
				return di
			}
			i = s + 1 // overlapping occurrences cannot matter, but stay correct
		}
	}
	return -1
}

// domainBounded reports whether an occurrence at [s,e) sits at host-label
// boundaries. Alphanumeric neighbors mean the occurrence is glued into a
// longer word ("notgrabify.link" left, "grabify.links" right) and is either a
// different host or benign prose - both released on purpose; every separator
// neighbor (space, dot, slash, colon, quotes, brackets) keeps the hit so
// quoted or listed domains still match.
func domainBounded(skel []byte, s, e int) bool {
	if s > 0 && isFloorAlnumByte(skel[s-1]) {
		return false
	}
	return e == len(skel) || !isFloorAlnumByte(skel[e])
}

func isFloorAlnumByte(b byte) bool {
	return 'a' <= b && b <= 'z' || '0' <= b && b <= '9'
}

// matchScam returns the index of the first ScamTerms phrase present in skel as
// an adjacent token sequence, or -1. One pass, manual token walking, zero
// allocations.
func matchScam(skel []byte) int {
	n := len(skel)
	for i := 0; i < n; {
		if !isFloorTokenByte(skel[i]) {
			i++
			continue
		}
		start := i
		for i < n && isFloorTokenByte(skel[i]) {
			i++
		}
		for pi := range scamPhrases {
			if phraseAt(skel, start, i, scamPhrases[pi]) {
				return pi
			}
		}
	}
	return -1
}

// phraseAt reports whether the phrase words match tokens starting at
// [start,end) and continuing across any separator run (space, punctuation).
func phraseAt(skel []byte, start, end int, words [][]byte) bool {
	i := end
	for wi, w := range words {
		if wi > 0 {
			start = nextToken(skel, &i)
			if start < 0 {
				return false
			}
			end = i
		}
		if end-start != len(w) || string(skel[start:end]) != string(w) {
			return false
		}
	}
	return true
}

// nextToken advances *i past the separator run following the previous token
// and reports the start of the next token, or -1 when the skeleton ends first.
func nextToken(skel []byte, i *int) int {
	n := len(skel)
	for *i < n && !isFloorTokenByte(skel[*i]) {
		*i++
	}
	if *i >= n {
		return -1
	}
	start := *i
	for *i < n && isFloorTokenByte(skel[*i]) {
		*i++
	}
	return start
}

// ---- clean-path pre-scan ---------------------------------------------------
//
// The automod's clean path (short, plain, unflagged line) exists to stay
// allocation-free, so it cannot run Normalize to call MatchFloor itself -
// without the pre-scan below, a bare short "grabify.link" or "get free nitro"
// bailed clean and skipped the immovable floor entirely. The pre-scan re-runs
// MatchFloor's exact semantics over a VIRTUAL skeleton: skelAt computes the
// byte Normalize would produce at each ascii position (lowercase; quorum-gated
// leet fold), and the walkers mirror matchDomain/matchScam over those bytes.
// Parity with the deep scan is therefore structural, not incidental - same
// patterns (already skeleton-space), same boundaries, same token rules.
//
// A hit is a ROUTING decision only: the caller takes the deep path, rebuilds
// the real skeleton and lets floorInfra's authoritative MatchFloor decide, so
// even an unexpected pre-scan hit costs one deep trip on a <=40-char line and
// nothing more. Non-ascii input never reaches here from the gate (the bail
// requires pure ascii); for other callers non-ascii bytes read as separators,
// which can only miss, never fabricate.

// MatchFloorPrescan reports which chat-floor infrastructure list raw text
// carries, without normalizing or allocating. Best-effort by contract, and
// one-directional BOTH ways (tightened 2026-08-22 after FuzzMatchFloor caught
// "0\x00grA81fY.l1nk" hitting the pre-scan while the authoritative scan
// released its skeleton "ograbify.link" - the control byte vanishes under
// Normalize, gluing the leading folded digit onto the host label):
//
//   - it may RELEASE shapes MatchFloor catches (non-ascii obfuscation);
//   - it may OVER-ROUTE shapes MatchFloor releases (strippable bytes glue
//     skeleton tokens that raw-text boundaries still split).
//
// Both directions are safe because a hit is ROUTING only: callers rebuild the
// skeleton and let MatchFloor decide authoritatively, so an over-route costs
// one deep trip on a short line and can never mint a verdict by itself.
func MatchFloorPrescan(text string) (FloorKind, string) {
	if di := matchDomainFolded(text); di >= 0 {
		return FloorIPLogger, IPLoggerDomains[di]
	}
	if si := matchScamFolded(text); si >= 0 {
		return FloorScam, ScamTerms[si]
	}
	return FloorNone, ""
}

// skelAt returns the virtual skeleton byte Normalize's ascii fast path would
// emit for text[pos]: letters lowercased; leet digits/symbols folded to their
// letter only when their whitespace token clears Normalize's two-letter
// quorum; every other byte passed through unchanged (separators included).
// tokStart must be the start of the whitespace token containing pos.
func skelAt(text string, tokStart, pos int) byte {
	b := text[pos]
	switch {
	case 'a' <= b && b <= 'z':
		return b
	case 'A' <= b && b <= 'Z':
		return b + ('a' - 'A')
	case isLeetFold(rune(b)) && asciiTokenLetters(text, tokStart, pos) >= 2:
		return byte(confusables[rune(b)])
	default:
		return b
	}
}

// tokenStartOf walks back to the beginning of the whitespace-delimited token
// containing pos. Normalize tracks this forward while building the skeleton;
// the pre-scan's per-candidate backward walk keeps every helper stateless at
// prescan scale (the gate only sends lines of at most 40 bytes here).
func tokenStartOf(text string, pos int) int {
	for pos > 0 && !isSkelSpace(text[pos-1]) {
		pos--
	}
	return pos
}

func isSkelSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\v' || b == '\f' || b == '\r'
}

// matchDomainFolded mirrors matchDomain over the virtual skeleton: first
// pattern byte as a cheap gate, full literal-in-skeleton comparison, then
// host-label boundary checks on the virtualized neighbors.
func matchDomainFolded(text string) int {
	for di, d := range ipLoggerPatterns {
		for i := 0; i+len(d) <= len(text); i++ {
			if skelAt(text, tokenStartOf(text, i), i) != d[0] {
				continue
			}
			if !eqAtVirtualSkel(text, i, d) {
				continue
			}
			if domainBoundedFolded(text, i, i+len(d)) {
				return di
			}
		}
	}
	return -1
}

// eqAtVirtualSkel reports whether the pattern occurs in text at offset s when
// read through the virtual skeleton.
func eqAtVirtualSkel(text string, s int, d []byte) bool {
	tokStart := tokenStartOf(text, s)
	for k, pb := range d {
		if skelAt(text, tokStart, s+k) != pb {
			return false
		}
	}
	return true
}

// domainBoundedFolded mirrors domainBounded: alphanumeric neighbors - judged
// on the virtual skeleton, where uppercase and quorum-folding leet digits have
// already become letters - glue the occurrence into a longer word and release
// it; separator neighbors keep the hit.
func domainBoundedFolded(text string, s, e int) bool {
	if s > 0 && isFloorAlnumFolded(text, s-1) {
		return false
	}
	return e == len(text) || !isFloorAlnumFolded(text, e)
}

func isFloorAlnumFolded(text string, pos int) bool {
	b := skelAt(text, tokenStartOf(text, pos), pos)
	return isFloorAlnumByte(b)
}

// matchScamFolded mirrors matchScam over the virtual skeleton: tokens are
// maximal [a-z] runs after folding, phrases must appear as adjacent whole
// tokens across any separator run.
func matchScamFolded(text string) int {
	n := len(text)
	for i := 0; i < n; {
		if !isFloorTokenFolded(text, i) {
			i++
			continue
		}
		start := i
		for i < n && isFloorTokenFolded(text, i) {
			i++
		}
		for pi := range scamPhrases {
			if phraseAtFolded(text, start, i, scamPhrases[pi]) {
				return pi
			}
		}
	}
	return -1
}

func isFloorTokenFolded(text string, pos int) bool {
	return isFloorTokenByte(skelAt(text, tokenStartOf(text, pos), pos))
}

// phraseAtFolded mirrors phraseAt: the words must match whole tokens starting
// at [start,end) and continuing across any separator run.
func phraseAtFolded(text string, start, end int, words [][]byte) bool {
	i := end
	for wi, w := range words {
		if wi > 0 {
			start = nextTokenFolded(text, &i)
			if start < 0 {
				return false
			}
			end = i
		}
		if end-start != len(w) || !eqAtVirtualSkel(text, start, w) {
			return false
		}
	}
	return true
}

// nextTokenFolded advances *i past the separator run following the previous
// token and reports the start of the next token, or -1 when the virtual
// skeleton ends first.
func nextTokenFolded(text string, i *int) int {
	n := len(text)
	for *i < n && !isFloorTokenFolded(text, *i) {
		*i++
	}
	if *i >= n {
		return -1
	}
	start := *i
	for *i < n && isFloorTokenFolded(text, *i) {
		*i++
	}
	return start
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

	if di := matchDomain(skel); di >= 0 {
		return IPLoggerDomains[di], true
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
