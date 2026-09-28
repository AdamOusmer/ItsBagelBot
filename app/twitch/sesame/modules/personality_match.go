// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strings"
)

// matchReaction returns the first reaction one of whose phrases occurs in the
// message at word boundaries. Most rows match the normalized text so "gn,
// @ItsBagelBot!!" reads as "gn itsbagelbot"; raw rows see the lowercased
// original, "@" and emoji intact.
//
// The gate runs first because everything after it is expensive and almost never
// pays: normalizeChat allocates three times (Map, Fields, Join) and the table
// expands to hundreds of phrases, each a full containsWord pass. Ordinary chat, which
// is nearly every line on the non-command path, now costs three substring scans
// and no allocation.
func matchReaction(raw string) (reaction, bool) {
	if !personalityGate.screens(raw) {
		return reaction{}, false
	}
	norm := normalizeChat(raw)
	for _, r := range personalityReactions {
		text := norm
		if r.matchRaw {
			text = raw
		}
		if matchesAny(text, r.phrases) {
			return r, true
		}
	}
	return reaction{}, false
}

// anchor is one gate term: a literal substring that every line matching some
// phrase in the table must also contain. Named rather than left a bare string
// because the derivation below moves anchors and phrases through the same
// shapes; a distinct type turns swapping the two into a compile error instead
// of a gate that screens for the wrong thing and silently kills a reaction.
type anchor string

// in reports whether a sits inside s. Substring, not word-boundary: the gate
// only has to be permissive enough never to drop a line the table would have
// matched, and containsWord's edge check costs more than the screen saves.
func (a anchor) in(s string) bool { return strings.Contains(s, string(a)) }

// phraseGate is the set of anchors a line is screened against before any
// reaction matching runs: a line holding none of them cannot match any phrase
// in the table, so matchReaction can return before it normalizes or scans
// anything.
type phraseGate []anchor

// screens reports whether s holds any of the gate's terms. Both users want the
// same question: matchReaction asks it of an incoming chat line, and the
// derivation asks it of a phrase to decide whether that phrase already has an
// anchor.
func (g phraseGate) screens(s string) bool {
	for _, a := range g {
		if a.in(s) {
			return true
		}
	}
	return false
}

// personalityGate is derived from personalityReactions rather than written
// out, so a new row cannot silently fall outside the gate and go dead. As of
// the current table it resolves to three terms: "bagel" (every name-bearing
// phrase), "bot" ("good bot" and "bad bot", which name no bagel) and the bare
// "🥯" of the emoji row.
var personalityGate = allPhrases(personalityReactions).buildGate()

// phraseSet is the table's phrases flattened into one corpus. The three
// derivation steps hang off it as methods rather than taking it as a second
// argument: as loose parameters they all shared the (string, []string) shape,
// which is one transposed call site away from counting phrases inside an
// anchor instead of anchors inside phrases.
type phraseSet []string

// allPhrases flattens the table's phrases into one corpus.
func allPhrases(rs []reaction) phraseSet {
	var out phraseSet
	for _, r := range rs {
		out = append(out, r.phrases...)
	}
	return out
}

// buildGate greedily covers every phrase with as few anchors as possible: walk
// the phrases, and whenever one is not already covered add its most widely
// shared anchor. Greedy is enough here because the phrases are one table of a
// known shape, not arbitrary input, and the result is checked into the comment
// on personalityGate.
func (ps phraseSet) buildGate() phraseGate {
	var g phraseGate
	for _, p := range ps {
		if g.screens(p) {
			continue
		}
		g = append(g, ps.bestAnchor(p))
	}
	return g
}

// bestAnchor picks the anchor of p that covers the most of the corpus, which
// is what collapses the 25 name variants onto the single term "bagel".
func (ps phraseSet) bestAnchor(p string) anchor {
	var best anchor
	bestN := -1
	for _, a := range phraseAnchors(p) {
		if n := ps.countCovered(a); n > bestN {
			best, bestN = a, n
		}
	}
	return best
}

// countCovered counts the phrases a would screen for.
func (ps phraseSet) countCovered(a anchor) int {
	n := 0
	for _, p := range ps {
		if a.in(p) {
			n++
		}
	}
	return n
}

// phraseAnchors returns the anchors of p: the substrings any matching line
// must also contain literally. Whole words qualify, since normalizeChat only
// turns non-word runes into spaces, so a word that survives into the
// normalized text was present verbatim in the raw line too, which is what the
// gate scans. A phrase with no word runes at all (the emoji row) anchors on
// itself, which is sound because that row matches the raw text anyway.
func phraseAnchors(p string) []anchor {
	words := strings.FieldsFunc(p, func(r rune) bool { return !isWordRune(r) })
	if len(words) == 0 {
		return []anchor{anchor(p)}
	}
	out := make([]anchor, len(words))
	for i, w := range words {
		out[i] = anchor(w)
	}
	return out
}

// matchesAny reports whether any phrase occurs in text at word boundaries.
func matchesAny(text string, phrases []string) bool {
	for _, p := range phrases {
		if containsWord(text, p) {
			return true
		}
	}
	return false
}

// normalizeChat flattens an already-lowercased chat line for phrase matching:
// every non-alphanumeric rune (punctuation, "@", emotes) becomes a space and
// runs of spaces collapse. "good night, @itsbagelbot!!" → "good night
// itsbagelbot", so phrases stay plain words and chat punctuates freely.
func normalizeChat(s string) string {
	mapped := strings.Map(func(r rune) rune {
		if isWordRune(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(mapped), " ")
}
