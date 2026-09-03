// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

// personalityGoldenOdds is the 1-in-N chance that any triggered reaction is
// replaced by the golden-bagel line.
const personalityGoldenOdds = 200

// pickIndex and goldenRoll are the module's randomness, hoisted to vars so
// tests can pin them. pickIndex draws pack lines, toast levels, mood rolls and
// the 1-in-N chance gates; goldenRoll decides the golden-bagel override.
var (
	pickIndex  = rand.IntN
	goldenRoll = func() bool { return rand.IntN(personalityGoldenOdds) == 0 }
)

// Personality is the bot's built-in voice: a fixed set of phrase reactions on
// the non-command chat path (praise, insults, pets, feeds, flips, a per-stream
// mood) plus a rotating bagel fun fact whenever chat @-mentions the bot.
// It is a named core module: always on, never listed on the dashboard, no
// config, not removable. The entire script lives in personality_lines.go.
//
// It deliberately does not touch the special-user greeting in Core; that path
// is personal and stays untouched.
func Personality(d engine.Deps) module.Module {
	m := module.NewModule("personality", module.KindCore)
	m.On("channel.chat.message", personalityOnChat(d))
	m.Command("bagels").Everyone().Aliases("fed", "bagelcount").
		Cooldown(feedCommandCooldown).Run(feedRankCommand(d))
	m.Command("bagelboard").Everyone().Aliases("feedboard", "bagellb").
		Cooldown(feedCommandCooldown).Run(feedBoardCommand(d))
	return m.Build()
}

// personalityReply renders one reaction's chat line. Implementations fall back
// to stateless randomness when the personality store is nil or erroring.
type personalityReply func(ctx context.Context, d engine.Deps, c *module.Context) string

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
	{name: "gn", phrases: withNames(botNames, "gn {name}", "goodnight {name}", "good night {name}", "night {name}", "bonne nuit {name}"), cooldown: 60 * time.Second, reply: packReply(personalityGnPack)},
	{name: "good", phrases: append(withNames(botNames, "good {name}"), "good bot"), cooldown: 15 * time.Second, reply: packReply(personalityGoodPack)},
	{name: "bad", phrases: append(withNames(botNames, "bad {name}"), "bad bot"), cooldown: 15 * time.Second, reply: packReply(personalityBadPack)},
	{name: "thanks", phrases: withNames(botNames, "thank you {name}", "thanks {name}", "ty {name}", "merci {name}"), cooldown: 15 * time.Second, reply: packReply(personalityThanksPack)},
	{name: "toast", phrases: withNames(botNames, "toast the {name}", "toast {name}"), cooldown: 30 * time.Second, reply: toastReply},
	{name: "pet", phrases: withNames(botNames, "pet the {name}", "pet {name}", "pets the {name}", "hug the {name}", "hug {name}", "hugs the {name}", "{name} hug"), cooldown: 30 * time.Second, reply: packReply(personalityAffectionPack)},
	{name: "feed", phrases: withNames(botNames, "feed the {name}", "feed {name}", "feeds the {name}"), cooldown: 30 * time.Second, reply: feedReply},
	{name: "boop", phrases: withNames(botNames, "boop the {name}", "boop {name}", "boops the {name}"), cooldown: 30 * time.Second, reply: packReply(personalityBoopPack)},
	{name: "mood", phrases: withNames(botNames, "{name} mood", "mood of the {name}"), cooldown: 60 * time.Second, reply: moodReply},
	{name: "give", phrases: []string{"give me a bagel", "i want a bagel", "gimme bagel", "gimme a bagel"}, cooldown: 30 * time.Second, reply: packReply(personalityGiveBagel)},
	{name: "emoji", phrases: []string{"🥯"}, cooldown: 90 * time.Second, oneIn: 12, matchRaw: true, reply: packReply(personalityEmojiPack)},
	{name: "fact", phrases: []string{botMention}, cooldown: 10 * time.Second, matchRaw: true, reply: factReply},
}

// personalityOnChat is the chat handler: screen the line, find the first
// matching reaction, pass the chance and cooldown gates, and emit one reply.
func personalityOnChat(d engine.Deps) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		text, ok := triggerCandidate(c)
		if !ok {
			return nil
		}
		r, ok := matchReaction(strings.ToLower(text))
		if !ok || !personalityAllowed(ctx, d, c, r) {
			return nil
		}
		msg := personalityLine(ctx, d, c, r)
		if msg == "" {
			return nil
		}
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          msg,
		})
		return nil
	}
}

// matchReaction returns the first reaction one of whose phrases occurs in the
// message at word boundaries. Most rows match the normalized text so "gn,
// @ItsBagelBot!!" reads as "gn itsbagelbot"; raw rows see the lowercased
// original, "@" and emoji intact.
//
// The gate runs first because everything after it is expensive and almost never
// pays: normalizeChat allocates three times (Map, Fields, Join) and the table
// expands to ~150 phrases, each a full containsWord pass. Ordinary chat, which
// is nearly every line on the non-command path, now costs three substring scans
// and no allocation.
func matchReaction(raw string) (reaction, bool) {
	if !personalityGated(raw) {
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

// personalityGate is the set of literal substrings that screen a chat line
// before any reaction matching runs: a line containing none of them cannot
// match any phrase in the table, so matchReaction can return before it
// normalizes or scans anything. Derived from personalityReactions rather than
// written out, so a new row cannot silently fall outside the gate and go dead.
// As of the current table it resolves to three terms: "bagel" (every
// name-bearing phrase), "bot" ("good bot" and "bad bot", which name no bagel)
// and the bare "🥟" of the emoji row.
var personalityGate = buildPersonalityGate(personalityReactions)

// personalityGated reports whether raw (already lowercased) holds any gate
// term. Substring, not word-boundary: the gate only has to be permissive enough
// never to drop a line the table would have matched.
func personalityGated(raw string) bool {
	for _, a := range personalityGate {
		if strings.Contains(raw, a) {
			return true
		}
	}
	return false
}

// buildPersonalityGate greedily covers every phrase with as few anchors as
// possible: walk the phrases, and whenever one is not already covered add its
// most widely shared anchor. Greedy is enough here because the phrases are one
// table of a known shape, not arbitrary input, and the result is checked into
// the comment above.
func buildPersonalityGate(rs []reaction) []string {
	phrases := allPhrases(rs)
	var gate []string
	for _, p := range phrases {
		if gateCovers(gate, p) {
			continue
		}
		gate = append(gate, bestAnchor(p, phrases))
	}
	return gate
}

// allPhrases flattens the table's phrases into one list.
func allPhrases(rs []reaction) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.phrases...)
	}
	return out
}

// phraseAnchors returns the substrings of p that any matching line must also
// contain literally. Whole words qualify: normalizeChat only turns non-word
// runes into spaces, so a word that survives into the normalized text was
// present verbatim in the raw line too, which is what the gate scans. A phrase
// with no word runes at all (the emoji row) anchors on itself, which is sound
// because that row matches the raw text anyway.
func phraseAnchors(p string) []string {
	words := strings.FieldsFunc(p, func(r rune) bool { return !isWordRune(r) })
	if len(words) > 0 {
		return words
	}
	return []string{p}
}

// gateCovers reports whether an already-chosen anchor sits inside p. Anchors are
// single words, so an anchor inside p lies inside one of p's words and cannot
// straddle a separator that normalization invented.
func gateCovers(gate []string, p string) bool {
	for _, a := range gate {
		if strings.Contains(p, a) {
			return true
		}
	}
	return false
}

// bestAnchor picks the anchor of p that covers the most phrases overall, which
// is what collapses the 25 name variants onto the single term "bagel".
func bestAnchor(p string, phrases []string) string {
	best, bestN := "", -1
	for _, a := range phraseAnchors(p) {
		if n := countCovered(a, phrases); n > bestN {
			best, bestN = a, n
		}
	}
	return best
}

// countCovered counts the phrases anchor would screen for.
func countCovered(anchor string, phrases []string) int {
	n := 0
	for _, p := range phrases {
		if strings.Contains(p, anchor) {
			n++
		}
	}
	return n
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

// personalityAllowed runs the reaction's chance gate, then claims its
// per-channel cooldown. A cooldown backend error fails closed: one skipped
// joke beats a spam loop when valkey is unhappy.
func personalityAllowed(ctx context.Context, d engine.Deps, c *module.Context, r reaction) bool {
	if r.oneIn > 1 && pickIndex(r.oneIn) != 0 {
		return false
	}
	if d.Cooldown == nil {
		return true
	}
	key := "personality:cd:" + r.name + ":" + strconv.FormatUint(c.BroadcasterID, 10)
	ok, err := d.Cooldown.Allow(ctx, key, r.cooldown)
	return err == nil && ok
}

// personalityLine renders the reaction's reply, letting the rare golden-bagel
// roll override any reaction with its own line.
func personalityLine(ctx context.Context, d engine.Deps, c *module.Context, r reaction) string {
	if goldenRoll() {
		return expandUser(personalityGoldenLine, c)
	}
	return r.reply(ctx, d, c)
}

// packReply builds a reply that draws one line from a fixed pack and expands
// its tokens.
func packReply(pack []string) personalityReply {
	return func(_ context.Context, _ engine.Deps, c *module.Context) string {
		return expandUser(pickLine(pack), c)
	}
}

// factReply serves the next fun fact on the channel's cursor, falling back to
// a random fact when the store is nil or unavailable.
func factReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	idx := pickIndex(len(personalityFacts))
	if d.Personality != nil {
		if cur, err := d.Personality.FactCursor(ctx, c.BroadcasterID); err == nil {
			idx = int((cur - 1) % int64(len(personalityFacts)))
		}
	}
	return personalityFacts[idx]
}

// feedReply records one feeding (the fleet-wide counters plus this channel's
// own row) and reports the fleet-wide numbers. The per-channel standing the
// same write produces is not printed here: !bagels and !bagelboard are the
// surfaces for it, and the reaction stays a one-line joke. No counts, no line:
// when the store is nil or erroring the reaction stays silent rather than
// answering without its numbers.
func feedReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	if d.Personality == nil {
		return ""
	}
	counts, err := d.Personality.Feed(ctx, c.BroadcasterID, c.Env.BroadcasterName())
	if err != nil {
		return ""
	}
	return fmt.Sprintf(pickLine(personalityFeedCountPack), counts.Today, counts.Total)
}

// moodReply reports the stream's mood, rolling a candidate that only sticks if
// the store accepts it first (first roll of the window wins fleet-wide).
func moodReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	mood := pickLine(personalityMoodPack)
	if d.Personality != nil {
		if m, err := d.Personality.Mood(ctx, c.BroadcasterID, mood); err == nil {
			mood = m
		}
	}
	return "current mood: " + mood
}

// toastReply rolls a toast level 0–10 and delivers its verdict.
func toastReply(_ context.Context, _ engine.Deps, _ *module.Context) string {
	level := pickIndex(len(personalityToastLines))
	return fmt.Sprintf(personalityToastLines[level], level)
}

// pickLine draws one line from a pack.
func pickLine(pack []string) string { return pack[pickIndex(len(pack))] }

// expandUser expands {user} to the chatter's display name; other tokens resolve
// through the shared dynamic vars ({random}, {choice:…}).
func expandUser(line string, c *module.Context) string {
	return module.ExpandString(line, func(key string) (string, bool) {
		if key == "user" {
			return strings.TrimPrefix(c.Env.ChatterName(), "@"), true
		}
		return module.ParseDynamic(key)
	})
}
