// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"math/rand/v2"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
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
// It is enabled by default and can be switched off for channels that only
// want a focused feature such as song requests. Its script lives in the shared
// i18n catalogs.
//
// It deliberately does not touch the special-user greeting in Core; that path
// is personal and stays untouched.
func Personality(d engine.Deps) module.Module {
	m := module.NewModule("personality", module.KindDefault)
	m.On("channel.chat.message", personalityOnChat(d))
	m.Command("bagels").Everyone().Aliases("fed", "bagelcount").
		Cooldown(feedCommandCooldown).Run(feedRankCommand(d))
	m.Command("bagelboard").Everyone().Aliases("feedboard", "bagellb").
		Cooldown(feedCommandCooldown).Run(feedBoardCommand(d))
	return m.Build()
}

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
		c.EnsureLocale(ctx)
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
