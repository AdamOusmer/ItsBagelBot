// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
)

// personalityReply renders one reaction's chat line. Implementations fall back
// to stateless randomness when the personality store is nil or erroring.
type personalityReply func(ctx context.Context, d engine.Deps, c *module.Context) string

// personalityAllowed runs the reaction's chance gate, then claims its
// per-channel cooldown. A cooldown backend error fails closed: one skipped
// joke beats a spam loop when valkey is unhappy.
func personalityAllowed(ctx context.Context, d engine.Deps, c *module.Context, r reaction) bool {
	if r.oneIn > 1 && pickIndex(r.oneIn) != 0 {
		return false
	}
	// Observation evaluates the same phrase and chance gates without claiming
	// a cooldown in the real broadcaster's Valkey namespace.
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

// personalityLine renders the reaction's reply, letting the rare golden-bagel
// roll override any reaction with its own line.
func personalityLine(ctx context.Context, d engine.Deps, c *module.Context, r reaction) string {
	if goldenRoll() {
		return expandUser(i18n.T(c.Locale, "personality.golden"), c)
	}
	return r.reply(ctx, d, c)
}

// packReply draws the same index in every locale and expands its tokens. The
// catalog-backed English pack supplies the length.
func packReply(name string, pack []string) personalityReply {
	return func(_ context.Context, _ engine.Deps, c *module.Context) string {
		return expandUser(personalityPackLine(c.Locale, name, pickIndex(len(pack))), c)
	}
}

func personalityPackLine(locale, name string, index int) string {
	return i18n.T(locale, "personality."+name+"."+strconv.Itoa(index))
}

// factReply serves the next fun fact on the channel's cursor, falling back to
// a random fact when the store is nil or unavailable.
func factReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	idx := pickIndex(len(personalityFacts))
	if d.Personality != nil && c.Env.Origin != "trial" {
		if cur, err := d.Personality.FactCursor(ctx, c.BroadcasterID); err == nil {
			idx = int((cur - 1) % int64(len(personalityFacts)))
		}
	}
	return personalityPackLine(c.Locale, "fact", idx)
}

// feedReply records one feeding (the fleet-wide counters plus this channel's
// own row) and reports the fleet-wide numbers. The per-channel standing the
// same write produces is not printed here: !bagels and !bagelboard are the
// surfaces for it, and the reaction stays a one-line joke. No counts, no line:
// when the store is nil or erroring the reaction stays silent rather than
// answering without its numbers.
func feedReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	// Feed is a write to fleet and channel counters. There is no read-only
	// equivalent of its post-increment totals, so a trial records the matched
	// reaction without inventing a count or touching the store.
	if c.Env.Origin == "trial" {
		return ""
	}
	if d.Personality == nil {
		return ""
	}
	eventID := c.Env.MsgID
	if eventID == "" {
		eventID = c.Env.ChatMessageID
	}
	if eventID == "" {
		eventID = c.Env.EventID
	}
	counts, err := d.Personality.Feed(ctx, c.BroadcasterID, c.Env.BroadcasterName(), eventID)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(personalityPackLine(c.Locale, "feed", pickIndex(len(personalityFeedCountPack))), counts.Today, counts.Total)
}

// moodReply reports the stream's mood, rolling a candidate that only sticks if
// the store accepts it first (first roll of the window wins fleet-wide).
func moodReply(ctx context.Context, d engine.Deps, c *module.Context) string {
	mood := pickLine(personalityMoodPack)
	if d.Personality != nil && c.Env.Origin != "trial" {
		if m, err := d.Personality.Mood(ctx, c.BroadcasterID, mood); err == nil {
			mood = m
		}
	}
	if index := slices.Index(personalityMoodPack, mood); index >= 0 {
		mood = personalityPackLine(c.Locale, "mood", index)
	}
	return i18n.T(c.Locale, "personality.mood.prefix") + mood
}

// toastReply rolls a toast level 0–10 and delivers its verdict.
func toastReply(_ context.Context, _ engine.Deps, c *module.Context) string {
	level := pickIndex(len(personalityToastLines))
	return fmt.Sprintf(personalityPackLine(c.Locale, "toast", level), level)
}

// pickLine draws one line from a pack.
func pickLine(pack []string) string { return pack[pickIndex(len(pack))] }

// expandUser expands {user} to the chatter's display name; other tokens resolve
// through the shared dynamic vars ({random}, {choice:…}).
func expandUser(line string, c *module.Context) string {
	return c.Palette("personality", "user", strings.TrimPrefix(c.Env.ChatterName(), "@")).ExpandString(line)
}
