// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"sort"

	"ItsBagelBot/app/twitch/sesame/module"
)

// ReplyTokenInventory is the Go half of the reply-token parity handshake
// (docs/specs/variables-catalog.md phase 5): every {token} a broadcaster's
// customizable reply template can actually have filled in, keyed by the same
// "<moduleId>.<replyKey>" namespace the kit catalog passes to replyTokens()
// as its hintNamespace argument (web/kit/lib/catalog/*.ts). Web CI has no Go
// toolchain and Go CI has no bun, so neither side can assert against the
// other's live code; reply_tokens_golden_test.go pins this map against a
// committed fixture that web/kit/lib/variables/parity.test.ts reads instead,
// mirroring how engine/scope/token_catalog_golden_test.go hands the kit
// variables manifest its own fixture.
//
// It used to be one hand-written map[string][]string literal — the token
// names lived here, duplicated from whatever switch or map actually filled
// them in over in alerts.go/urchin.go/mcsr_ranked.go/etc, free to drift from
// it. It is derived now, from replySpecs below: every namespace is a
// module.Spec, and a Spec's Names() is what this function copies out. A Spec
// entry that also carries a Doc documents what the token means for the kit
// catalog to surface (web/kit/lib/catalog/*.ts's replyTokens hint text); one
// that does not (the stats family, see replySpecs' comment) still keeps this
// function honest about WHICH names exist, even where writing the doc prose
// per name was left for a later pass.
func ReplyTokenInventory() map[string][]string {
	out := make(map[string][]string, len(replySpecs))
	for ns, spec := range replySpecs {
		names := spec.Names()
		// Sorted here, at the one choke point every namespace passes
		// through, rather than trusted to already be sorted: a "derived"
		// entry's names (case 1, mcsrRecordTokens.Names() and friends) come
		// off a Go map, whose iteration order is randomized per run. Without
		// this, two calls in the same process could disagree with each
		// other, never mind with the committed golden file.
		sort.Strings(names)
		out[ns] = names
	}
	return out
}

// replySpecs backs ReplyTokenInventory. Three different ways a namespace
// gets in here, in decreasing order of how "derived" it really is:
//
//  1. Off a package-level module.TokenExpander var's own Names() — the exact
//     map the runtime resolves against, so this cannot name a token the
//     handler does not also fill in. (mcsr.record/lb/race/pace/nethers,
//     fortnite.stats)
//  2. A module.Spec with Name+Doc, declared beside the code that migrated
//     onto module.Palette/module.KV in this change. (alerts, shoutout, time,
//     queue, the reward trio)
//  3. A pinned module.Spec (names only, no Doc) for a reply whose values are
//     built by an inline TokenExpander literal or a StringPalette function
//     (urchin's four externalCommand definitions; mcsr's elo/session/
//     lastmatch/pb, which are locale-dependent StringPalette funcs, not
//     static maps) — extracting each into a named, callable declaration
//     would touch ~10 more files for no runtime change, since both
//     TokenExpander.Expand and StringPalette.Expand already route their miss
//     path through the same pure family (module/vars.go's pureFallback) as
//     every Palette does. reply_tokens_test.go is what actually keeps these
//     honest: it runs the real handler and fails if a pinned name is not
//     really filled in.
//
// builtin.clip is its own case, documented at its own entry: outgress (a
// different app's package, not importable here) is what actually resolves
// it, so this package can only pin the names its own tests can confirm pass
// through untouched.
//
// A kit ModuleReply/BuiltinCommandDef that names no namespace here has no Go
// counterpart yet: chat does not answer it. As of this writing that is every
// reply in codm.ts, clashroyale.ts, valorant.ts, games.ts and raffle.ts, plus
// alerts.gift, alerts.ads, alerts.timeout, urchin.weekly, urchin.monthly,
// urchin.tagdescription, mcsr.lastfort, fortnite.season and fortnite.session,
// and every builtin-commands.ts reply besides clip.
var replySpecs = map[string]module.Spec{
	"alerts.follow": {Entries: []module.SpecEntry{
		{Name: "user", Doc: "the new follower's display name"},
	}},
	"alerts.sub": {Entries: []module.SpecEntry{
		{Name: "tier", Doc: "the subscription tier (1000/2000/3000, or Prime)"},
		{Name: "user", Doc: "the subscriber's display name"},
	}},
	"alerts.cheer": {Entries: []module.SpecEntry{
		{Name: "bits", Doc: "the number of bits cheered"},
		{Name: "user", Doc: "the cheerer's display name, or \"An anonymous cheerer\""},
	}},
	"alerts.raid": {Entries: []module.SpecEntry{
		{Name: "user", Doc: "the raiding channel's display name"},
		{Name: "viewers", Doc: "the number of viewers the raid brought"},
	}},

	// See the package doc above: outgress owns the actual substitution, so
	// this package can only pin the names and let reply_tokens_test.go
	// confirm they pass through Output.Template untouched.
	"builtin.clip": {Entries: []module.SpecEntry{
		{Name: "clip", Doc: "the created clip's URL"},
		{Name: "target", Doc: "the clipped streamer's display name"},
		{Name: "user", Doc: "the invoking chatter's display name"},
	}},

	"shoutout.shoutout": {Entries: []module.SpecEntry{
		{Name: "raider", Doc: "the raiding channel's display name"},
		{Name: "raider.login", Doc: "the raiding channel's login, for a twitch.tv/ link"},
		{Name: "viewers", Doc: "the number of viewers the raid brought"},
	}},

	"time.time": {Entries: []module.SpecEntry{
		{Name: "date", Doc: "the local date, e.g. \"Monday, January 2\""},
		{Name: "time", Doc: "the local clock time, formatted per the module's Format setting"},
		{Name: "timezone", Doc: "the resolved timezone name or offset"},
		{Name: "user", Doc: "the invoking chatter's display name"},
	}},
	// timeReplySpec (timeofday.go) is the single expander that answers both
	// this namespace and the next; time.lookup only differs from time.time
	// in adding {place}, since a place lookup can name where it resolved to.
	"time.lookup": {Entries: []module.SpecEntry{
		{Name: "date", Doc: "the local date at the looked-up place"},
		{Name: "time", Doc: "the local clock time at the looked-up place"},
		{Name: "timezone", Doc: "the looked-up place's timezone name or offset"},
		{Name: "place", Doc: "the normalized place name the lookup resolved"},
		{Name: "user", Doc: "the invoking chatter's display name"},
	}},

	// withCommon: queue.go answers through chatReplier.reply (reply.go),
	// which composes module.Common(c) ahead of its own kv pairs (see
	// commonEntries' doc below) — so {channel}, and {user} where a namespace
	// does not already declare its own, resolve on every chatReplier reply,
	// not only the pair its own call happens to pass.
	"queue.join": {Entries: withCommon([]module.SpecEntry{
		{Name: "pos", Doc: "the chatter's new position in the queue"},
		{Name: "user", Doc: "the invoking chatter's login"},
	})},
	"queue.next": {Entries: withCommon([]module.SpecEntry{
		{Name: "count", Doc: "how many entries remain in the queue"},
		{Name: "target", Doc: "the login pulled off the front of the queue"},
	})},

	// The reward trio: three channel-points-redemption surfaces that share
	// the one sanitized {input} entry (sanitizeRewardInput, channelpoints.go).
	// None of these goes through chatReplier/Common — each builds its own
	// module.KV directly (channelpoints.go, govee.go, songqueue_redeem.go) —
	// so, unlike queue above, they do NOT get {channel} or {user} for free;
	// each lists only the names its own KV call actually binds.
	"channelpoints.reply": {Entries: []module.SpecEntry{
		{Name: "user", Doc: "the redeemer's display name"},
		{Name: "input", Doc: "the text the redeemer typed, sanitized (see sanitizeRewardInput)"},
		{Name: "reward", Doc: "the reward's title"},
		{Name: "cost", Doc: "the reward's point cost"},
		{Name: "channel", Doc: "the broadcaster's login"},
		{Name: "counter", Doc: "the bound counter's new value (only when the binding has one)"},
		{Name: "points", Doc: "the loyalty points the binding awards (only when positive)"},
	}},
	"govee.reply": {Entries: []module.SpecEntry{
		{Name: "user", Doc: "the redeemer's display name"},
		{Name: "input", Doc: "the text the redeemer typed, sanitized (see sanitizeRewardInput)"},
		{Name: "color", Doc: "the resolved colour name"},
	}},
	"songqueue.redeem": {Entries: []module.SpecEntry{
		{Name: "user", Doc: "the redeemer's display name"},
		{Name: "track", Doc: "the queued track's title"},
		{Name: "input", Doc: "the text the redeemer typed, sanitized (see sanitizeRewardInput)"},
		{Name: "pos", Doc: "the track's position in the queue"},
	}},

	// Pinned: see replySpecs' doc, case 3. Names mirror the
	// module.TokenExpander literals in urchin.go's four externalCommand
	// definitions (urchinSessionRun's !daily/!weekly/!monthly share one
	// shape, hence urchin.daily standing in for all three).
	"urchin.daily": {Entries: []module.SpecEntry{
		{Name: "beds"}, {Name: "finaldeaths"}, {Name: "finals"}, {Name: "fkdr"},
		{Name: "games"}, {Name: "levels"}, {Name: "losses"}, {Name: "player"}, {Name: "wins"},
	}},
	"urchin.stats": {Entries: []module.SpecEntry{
		{Name: "beds"}, {Name: "finaldeaths"}, {Name: "finals"}, {Name: "fkdr"},
		{Name: "losses"}, {Name: "player"}, {Name: "stars"}, {Name: "wins"}, {Name: "wlr"},
	}},
	"urchin.sniper": {Entries: []module.SpecEntry{
		{Name: "mode"}, {Name: "player"}, {Name: "score"}, {Name: "tagcount"},
	}},
	"urchin.tags": {Entries: []module.SpecEntry{
		{Name: "player"}, {Name: "tagcount"}, {Name: "tags"},
	}},

	// Derived: mcsrRecordTokens/mcsrLbTokens/mcsrRaceTokens are package-level
	// module.TokenExpander vars (mcsr_ranked.go); .Names() reads the exact
	// map the runtime resolves against.
	"mcsr.record": {Entries: namesOnly(mcsrRecordTokens.Names())},
	"mcsr.lb":     {Entries: namesOnly(mcsrLbTokens.Names())},
	"mcsr.race":   {Entries: namesOnly(mcsrRaceTokens.Names())},

	// Pinned: see replySpecs' doc, case 3 — mcsrEloPalette/mcsrSessionPalette/
	// mcsrLastMatchPalette (mcsr_ranked.go) and the pb command build a
	// module.StringPalette per call against the channel's locale, so there is
	// no static map to read names off.
	"mcsr.elo": {Entries: []module.SpecEntry{
		{Name: "country"}, {Name: "draws"}, {Name: "elo"}, {Name: "losses"},
		{Name: "matches"}, {Name: "player"}, {Name: "rank"}, {Name: "wins"},
	}},
	"mcsr.session": {Entries: []module.SpecEntry{
		{Name: "draws"}, {Name: "elo"}, {Name: "elochange"}, {Name: "losses"},
		{Name: "matches"}, {Name: "player"}, {Name: "wins"},
	}},
	"mcsr.lastmatch": {Entries: []module.SpecEntry{
		{Name: "ago"}, {Name: "elochange"}, {Name: "opponent"}, {Name: "player"},
		{Name: "result"}, {Name: "seed"}, {Name: "structure"}, {Name: "time"},
	}},
	"mcsr.pb": {Entries: []module.SpecEntry{
		{Name: "player"}, {Name: "time"}, {Name: "window"},
	}},

	// Derived: mcsrPaceTokens/mcsrNethersTokens are package-level
	// module.TokenExpander vars (mcsr_pace.go).
	"mcsr.pace":    {Entries: namesOnly(mcsrPaceTokens.Names())},
	"mcsr.nethers": {Entries: namesOnly(mcsrNethersTokens.Names())},

	// Derived: fortniteStatsTokens() (fortnite.go) takes no reply — it is a
	// static map built once — so calling it here for its Names() reads the
	// same declaration fortniteStatsRun expands templates against.
	"fortnite.stats": {Entries: namesOnly(fortniteStatsTokens().Names())},

	// Pinned: fortniteStoreRun (fortnite.go) binds a fresh module.KV per
	// call ({items} needs the channel's locale, which a static declaration
	// cannot reach).
	"fortnite.store": {Entries: []module.SpecEntry{
		{Name: "count"}, {Name: "date"}, {Name: "items"},
	}},
}

// namesOnly wraps a bare name list into SpecEntry form, for the "derived but
// undocumented" case (see replySpecs' doc, case 1): a TokenExpander var has
// no doc strings to read, only keys.
func namesOnly(names []string) []module.SpecEntry {
	out := make([]module.SpecEntry, len(names))
	for i, name := range names {
		out[i] = module.SpecEntry{Name: name}
	}
	return out
}

// commonEntries mirrors module.Common (module/vars.go) for the inventory:
// {user} the invoking chatter's login, {channel} the broadcaster's login.
// chatReplier.reply (reply.go) and feedText (personality_feed.go) both
// compose module.Common(c) ahead of their own kv pairs, so every reply built
// through either one resolves both names whether or not its own call passes
// them — a caller only needs a kv pair for "user" when it wants something
// OTHER than the login (a display name; see module.Common's doc).
var commonEntries = []module.SpecEntry{
	{Name: "user", Doc: "the invoking chatter's login"},
	{Name: "channel", Doc: "the broadcaster's login"},
}

// withCommon appends commonEntries to entries for a chatReplier/feedText
// namespace, skipping a name entries already declares — queue.join already
// lists "user" with the identical meaning Common gives it, and duplicating
// it would print the name twice in ReplyTokenInventory(). This is the
// "derive it" half of Common's reach: a namespace states only what its own
// kv call adds, and gets {user}/{channel} for free the same way the runtime
// does, instead of every such namespace hand-typing "channel" into its own
// entry list.
func withCommon(entries []module.SpecEntry) []module.SpecEntry {
	have := make(map[string]bool, len(entries))
	for _, e := range entries {
		have[e.Name] = true
	}
	out := entries
	for _, c := range commonEntries {
		if !have[c.Name] {
			out = append(out, c)
		}
	}
	return out
}
