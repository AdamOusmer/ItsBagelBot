// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strings"
	"time"
)

// reaction is one row of the personality table: the phrases that trip it, the
// per-channel cooldown that keeps it charming instead of spammy, an optional
// 1-in-N chance gate for ambient reactions, and the reply renderer. matchRaw
// rows match against the raw lowercased message instead of the normalized one
// (needed for the 🥯 emoji and the "@" of a mention, both of which
// normalization would strip).
type reaction struct {
	name     string
	phrases  []string
	cooldown time.Duration
	oneIn    int
	matchRaw bool
	reply    personalityReply
}

// botNames are every way chat addresses the bot, bare "bagel" included; a
// directed reaction ("good {name}", "feed the {name}") accepts any of them.
var botNames = []string{"bagel", "bagelbot", "bagel bot", "itsbagelbot", "its bagel bot"}

// botMention is the literal Twitch @-mention of the bot, and the only thing
// that serves a fun fact. Written out in chat ("bagelbot", "bagel fact") it is
// just a word about a breakfast food; the "@" is the part that means someone is
// talking to the bot, so the fact row matches the raw text to keep it.
const botMention = "@itsbagelbot"

// withNames expands "{name}" in each pattern across the given name list, so a
// reaction declares its shape once ("feed the {name}") and every way of
// addressing the bot comes along naturally.
func withNames(names []string, patterns ...string) []string {
	out := make([]string, 0, len(patterns)*len(names))
	for _, p := range patterns {
		for _, n := range names {
			out = append(out, strings.ReplaceAll(p, "{name}", n))
		}
	}
	return out
}

// personalityReactions is scanned in order and the first match wins, so the
// specific interactions sit above the generic mention→fact row: "good night
// @itsbagelbot" lands on the goodnight, "good bagel bot" on praise, and only a
// bare "@itsbagelbot" falls through to a fun fact. gn sits above good so an
// explicit goodnight always beats a praise phrase sharing the line. Phrases
// are lowercase; matching is word-boundary via containsWord on normalized
// text (see normalizeChat), except the raw-text emoji and fact rows.
//
// Order is load-bearing and cannot be traded for speed. The obvious speedup, a
// single Aho-Corasick pass over every phrase (internal/moderation has one), was
// rejected: its automaton reports whichever pattern ends earliest in the text,
// so "good bagel, gn bagel" would answer praise where this table answers
// goodnight, and it reports a pattern index without the byte offsets
// containsWord needs to check word edges. personalityGate below is the cheap
// screen used instead.
var personalityReactions = []reaction{
	{name: "gn", phrases: withNames(botNames, "gn {name}", "goodnight {name}", "good night {name}", "night {name}", "bonne nuit {name}"), cooldown: 60 * time.Second, reply: packReply("gn", personalityGnPack)},
	{name: "good", phrases: append(withNames(botNames, "good {name}", "bon {name}", "bravo {name}"), "good bot", "bon bot"), cooldown: 15 * time.Second, reply: packReply("good", personalityGoodPack)},
	{name: "bad", phrases: append(withNames(botNames, "bad {name}", "mauvais {name}"), "bad bot", "mauvais bot"), cooldown: 15 * time.Second, reply: packReply("bad", personalityBadPack)},
	{name: "thanks", phrases: withNames(botNames, "thank you {name}", "thanks {name}", "ty {name}", "merci {name}"), cooldown: 15 * time.Second, reply: packReply("thanks", personalityThanksPack)},
	{name: "toast", phrases: withNames(botNames, "toast the {name}", "toast {name}", "grille le {name}", "grille {name}"), cooldown: 30 * time.Second, reply: toastReply},
	{name: "pet", phrases: withNames(botNames, "pet the {name}", "pet {name}", "pets the {name}", "hug the {name}", "hug {name}", "hugs the {name}", "{name} hug", "caresse le {name}", "câlin {name}"), cooldown: 30 * time.Second, reply: packReply("affection", personalityAffectionPack)},
	{name: "feed", phrases: withNames(botNames, "feed the {name}", "feed {name}", "feeds the {name}", "nourris le {name}", "nourris {name}"), cooldown: 30 * time.Second, reply: feedReply},
	{name: "boop", phrases: withNames(botNames, "boop the {name}", "boop {name}", "boops the {name}"), cooldown: 30 * time.Second, reply: packReply("boop", personalityBoopPack)},
	{name: "mood", phrases: withNames(botNames, "{name} mood", "mood of the {name}", "humeur du {name}", "humeur {name}"), cooldown: 60 * time.Second, reply: moodReply},
	{name: "give", phrases: []string{"give me a bagel", "i want a bagel", "gimme bagel", "gimme a bagel", "donne moi un bagel", "je veux un bagel"}, cooldown: 30 * time.Second, reply: packReply("give", personalityGiveBagel)},
	{name: "emoji", phrases: []string{"🥯"}, cooldown: 90 * time.Second, oneIn: 12, matchRaw: true, reply: packReply("emoji", personalityEmojiPack)},
	{name: "fact", phrases: []string{botMention}, cooldown: 10 * time.Second, matchRaw: true, reply: factReply},
}

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
