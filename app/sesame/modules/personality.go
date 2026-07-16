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
// mood) plus a rotating bagel fun fact whenever the bot itself is mentioned.
// It is a named core module: always on, never listed on the dashboard, no
// config, not removable. The entire script lives in personality_lines.go.
//
// It deliberately does not touch the special-user greeting in Core; that path
// is personal and stays untouched.
func Personality(d engine.Deps) module.Module {
	m := module.NewModule("personality", module.KindCore)
	m.On("channel.chat.message", personalityOnChat(d))
	return m.Build()
}

// personalityReply renders one reaction's chat line. Implementations fall back
// to stateless randomness when the personality store is nil or erroring.
type personalityReply func(ctx context.Context, d engine.Deps, c *module.Context) string

// reaction is one row of the personality table: the phrases that trip it, the
// per-channel cooldown that keeps it charming instead of spammy, an optional
// 1-in-N chance gate for ambient reactions, and the reply renderer.
type reaction struct {
	name     string
	phrases  []string
	cooldown time.Duration
	oneIn    int
	reply    personalityReply
}

// personalityReactions is scanned in order and the first match wins, so the
// specific interactions sit above the generic mention→fact row: "good bagel
// bot" lands on praise, not on a fun fact. Phrases are lowercase; matching is
// word-boundary via containsWord.
var personalityReactions = []reaction{
	{name: "good", phrases: []string{"good bagel", "good bot", "good bagelbot"}, cooldown: 15 * time.Second, reply: packReply(personalityGoodPack)},
	{name: "bad", phrases: []string{"bad bagel", "bad bot", "bad bagelbot"}, cooldown: 15 * time.Second, reply: packReply(personalityBadPack)},
	{name: "thanks", phrases: []string{"thank you bagel", "thanks bagel", "ty bagel", "merci bagel"}, cooldown: 15 * time.Second, reply: packReply(personalityThanksPack)},
	{name: "toast", phrases: []string{"toast the bagel", "toast bagel"}, cooldown: 30 * time.Second, reply: toastReply},
	{name: "pet", phrases: []string{"pet the bagel", "pet bagel", "pets the bagel", "hug the bagel", "hug bagel", "hugs the bagel", "bagel hug"}, cooldown: 30 * time.Second, reply: packReply(personalityAffectionPack)},
	{name: "feed", phrases: []string{"feed the bagel", "feed bagel", "feeds the bagel"}, cooldown: 30 * time.Second, reply: feedReply},
	{name: "boop", phrases: []string{"boop the bagel", "boop bagel", "boops the bagel"}, cooldown: 30 * time.Second, reply: packReply(personalityBoopPack)},
	{name: "gn", phrases: []string{"gn bagel", "goodnight bagel", "good night bagel", "bonne nuit bagel"}, cooldown: 60 * time.Second, reply: packReply(personalityGnPack)},
	{name: "mood", phrases: []string{"bagel mood", "mood of the bagel"}, cooldown: 60 * time.Second, reply: moodReply},
	{name: "emoji", phrases: []string{"🥯"}, cooldown: 90 * time.Second, oneIn: 12, reply: packReply(personalityEmojiPack)},
	{name: "fact", phrases: []string{"@itsbagelbot", "itsbagelbot", "bagelbot", "bagel bot", "bagel fact"}, cooldown: 10 * time.Second, reply: factReply},
	{name: "give", phrases: []string{"give me a bagel", "I want a bagel", "gimme bagel", "gimme a bagel"}, cooldown: 30 * time.Second, reply: packReply(personalityGiveBagel)},
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
// lowercased message at word boundaries.
func matchReaction(text string) (reaction, bool) {
	for _, r := range personalityReactions {
		for _, p := range r.phrases {
			if containsWord(text, p) {
				return r, true
			}
		}
	}
	return reaction{}, false
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

// feedReply records one feeding on the global counters (one bagel, shared by
// every channel: a permanent DB total plus a valkey today window) and reports
// both. No counts, no line: when the store is nil or erroring the reaction
// stays silent rather than answering without its numbers.
func feedReply(ctx context.Context, d engine.Deps, _ *module.Context) string {
	if d.Personality == nil {
		return ""
	}
	counts, err := d.Personality.Feed(ctx)
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
