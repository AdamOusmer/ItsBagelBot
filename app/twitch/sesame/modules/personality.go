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

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

const personalityGoldenOdds = 200

var (
	pickIndex  = rand.IntN
	goldenRoll = func() bool { return rand.IntN(personalityGoldenOdds) == 0 }
)

func Personality(d engine.Deps) module.Module {
	m := module.NewModule("personality", module.KindDefault)
	m.On("channel.chat.message", personalityOnChat(d))
	m.Command("bagels").Everyone().Aliases("fed", "bagelcount").
		Cooldown(feedCommandCooldown).Run(feedRankCommand(d))
	m.Command("bagelboard").Everyone().Aliases("feedboard", "bagellb").
		Cooldown(feedCommandCooldown).Run(feedBoardCommand(d))
	return m.Build()
}

type personalityReply func(ctx context.Context, d engine.Deps, c *module.Context) string

type reaction struct {
	name     string
	phrases  []string
	cooldown time.Duration
	oneIn    int
	matchRaw bool
	reply    personalityReply
}

var botNames = []string{"bagel", "bagelbot", "bagel bot", "itsbagelbot", "its bagel bot"}

const botMention = "@itsbagelbot"

func withNames(names []string, patterns ...string) []string {
	out := make([]string, 0, len(patterns)*len(names))
	for _, p := range patterns {
		for _, n := range names {
			out = append(out, strings.ReplaceAll(p, "{name}", n))
		}
	}
	return out
}

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

type anchor string

func (a anchor) in(s string) bool { return strings.Contains(s, string(a)) }

type phraseGate []anchor

func (g phraseGate) screens(s string) bool {
	for _, a := range g {
		if a.in(s) {
			return true
		}
	}
	return false
}

var personalityGate = allPhrases(personalityReactions).buildGate()

type phraseSet []string

func allPhrases(rs []reaction) phraseSet {
	var out phraseSet
	for _, r := range rs {
		out = append(out, r.phrases...)
	}
	return out
}

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

func (ps phraseSet) countCovered(a anchor) int {
	n := 0
	for _, p := range ps {
		if a.in(p) {
			n++
		}
	}
	return n
}

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

func matchesAny(text string, phrases []string) bool {
	for _, p := range phrases {
		if containsWord(text, p) {
			return true
		}
	}
	return false
}

func normalizeChat(s string) string {
	mapped := strings.Map(func(r rune) rune {
		if isWordRune(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(mapped), " ")
}

func personalityAllowed(ctx context.Context, d engine.Deps, c *module.Context, r reaction) bool {
	if r.oneIn > 1 && pickIndex(r.oneIn) != 0 {
		return false
	}
	if c.Env.Origin == "trial" {
		return true
	}
	if d.Cooldown == nil {
		return true
	}
	key := "personality:cd:" + r.name + ":" + strconv.FormatUint(c.BroadcasterID, 10)
	ok, err := d.Cooldown.Allow(ctx, key, r.cooldown)
	return err == nil && ok
}

func personalityLine(ctx context.Context, d engine.Deps, c *module.Context, r reaction) string {
	if goldenRoll() {
		return expandUser(personalityGoldenLine, c)
	}
	return r.reply(ctx, d, c)
}

func packReply(pack []string) personalityReply {
	return func(_ context.Context, _ engine.Deps, c *module.Context) string {
		return expandUser(pickLine(pack), c)
	}
}

func factReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	idx := pickIndex(len(personalityFacts))
	if d.Personality != nil && c.Env.Origin != "trial" {
		if cur, err := d.Personality.FactCursor(ctx, c.BroadcasterID); err == nil {
			idx = int((cur - 1) % int64(len(personalityFacts)))
		}
	}
	return personalityFacts[idx]
}

func feedReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	if c.Env.Origin == "trial" {
		return ""
	}
	if d.Personality == nil {
		return ""
	}
	counts, err := d.Personality.Feed(ctx, c.BroadcasterID, c.Env.BroadcasterName())
	if err != nil {
		return ""
	}
	return fmt.Sprintf(pickLine(personalityFeedCountPack), counts.Today, counts.Total)
}

func moodReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	mood := pickLine(personalityMoodPack)
	if d.Personality != nil && c.Env.Origin != "trial" {
		if m, err := d.Personality.Mood(ctx, c.BroadcasterID, mood); err == nil {
			mood = m
		}
	}
	return "current mood: " + mood
}

func toastReply(_ context.Context, _ engine.Deps, _ *module.Context) string {
	level := pickIndex(len(personalityToastLines))
	return fmt.Sprintf(personalityToastLines[level], level)
}

func pickLine(pack []string) string { return pack[pickIndex(len(pack))] }

func expandUser(line string, c *module.Context) string {
	return module.KV("user", strings.TrimPrefix(c.Env.ChatterName(), "@")).WithLocale(module.Locale(c.Locale)).ExpandString(line)
}
