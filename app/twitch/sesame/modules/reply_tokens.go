// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

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
// Each namespace mirrors one map or switch in the named file:
//   - alerts.follow/sub/cheer/raid: the per-alert map[string]string literals
//     built in alerts.go's render closures.
//   - shoutout.shoutout: the token switch in shoutout.go.
//   - time.time: the token switch in timeofday.go's expandTimeTemplate.
//   - queue.join/next: the kv pairs reply.go's chatReplier.reply is given in
//     queue.go, plus the {user} it always makes available.
//   - urchin.daily/stats/sniper/tags: the module.TokenExpander maps in
//     urchin.go.
//   - mcsr.elo/session/lastmatch/record/lb/race/pb: the module.StringPalette
//     and module.TokenExpander values built in mcsr_ranked.go.
//   - mcsr.pace/nethers: the module.TokenExpander maps in mcsr_pace.go.
//   - fortnite.stats/store: the module.TokenExpander map and token switch in
//     fortnite.go.
//   - builtin.clip: the {clip}/{user}/{target} names clip.go's Output.Template
//     carries through unexpanded: outgress (a different app's package, not
//     importable here) is what actually fills them in, so reply_tokens_test.go
//     can only assert the template passes through this package untouched.
//
// reply_tokens_test.go exercises the real handler behind every namespace here
// (except builtin.clip, for the reason above) and asserts every name below
// actually gets substituted, so this map cannot drift from the code that
// fills it in.
//
// A kit ModuleReply/BuiltinCommandDef that names no namespace here has no Go
// counterpart yet: chat does not answer it. As of this writing that is every
// reply in codm.ts, clashroyale.ts, valorant.ts, games.ts and raffle.ts, plus
// alerts.gift, alerts.ads, alerts.timeout, urchin.weekly, urchin.monthly,
// urchin.tagdescription, mcsr.lastfort, fortnite.season, fortnite.session and
// every builtin-commands.ts reply besides clip.
func ReplyTokenInventory() map[string][]string {
	out := make(map[string][]string, len(replyTokenInventory))
	for ns, names := range replyTokenInventory {
		cp := make([]string, len(names))
		copy(cp, names)
		out[ns] = cp
	}
	return out
}

// replyTokenInventory backs ReplyTokenInventory; every slice is sorted so the
// golden fixture (and a kit-side comparison against it) never depends on
// this literal's own field order.
var replyTokenInventory = map[string][]string{
	"alerts.follow": {"user"},
	"alerts.sub":    {"tier", "user"},
	"alerts.cheer":  {"bits", "user"},
	"alerts.raid":   {"user", "viewers"},

	"builtin.clip": {"clip", "target", "user"},

	"shoutout.shoutout": {"raider", "raider.login", "viewers"},

	"time.time": {"date", "time", "timezone", "user"},

	"queue.join": {"pos", "user"},
	"queue.next": {"count", "target"},

	"urchin.daily":  {"beds", "finaldeaths", "finals", "fkdr", "games", "levels", "losses", "player", "wins"},
	"urchin.stats":  {"beds", "finaldeaths", "finals", "fkdr", "losses", "player", "stars", "wins", "wlr"},
	"urchin.sniper": {"mode", "player", "score", "tagcount"},
	"urchin.tags":   {"player", "tagcount", "tags"},

	"mcsr.elo":       {"country", "draws", "elo", "losses", "matches", "player", "rank", "wins"},
	"mcsr.session":   {"draws", "elo", "elochange", "losses", "matches", "player", "wins"},
	"mcsr.pace":      {"bastion", "end", "finish", "firstportal", "firststructure", "fortress", "nether", "nethers", "nph", "player", "secondstructure", "stronghold"},
	"mcsr.nethers":   {"nether", "nethers", "nph", "player"},
	"mcsr.lastmatch": {"ago", "elochange", "opponent", "player", "result", "seed", "structure", "time"},
	"mcsr.record":    {"played", "playera", "playerb", "winsa", "winsb"},
	"mcsr.lb":        {"board", "list"},
	"mcsr.race":      {"leader", "leadertime", "player", "rank", "time"},
	"mcsr.pb":        {"player", "time", "window"},

	"fortnite.stats": {
		"duokd", "duomatches", "duowins", "kd", "kills", "matches", "player",
		"solokd", "solomatches", "solowins", "squadkd", "squadmatches", "squadwins",
		"window", "winrate", "wins",
	},
	"fortnite.store": {"count", "date", "items"},
}
