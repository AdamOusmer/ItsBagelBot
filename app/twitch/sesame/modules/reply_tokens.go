// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"sort"

	"ItsBagelBot/app/twitch/sesame/module"
)

func ReplyTokenInventory() map[string][]string {
	out := make(map[string][]string, len(replySpecs))
	for ns, spec := range replySpecs {
		names := spec.Names()
		sort.Strings(names)
		out[ns] = names
	}
	return out
}

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
	"time.lookup": {Entries: []module.SpecEntry{
		{Name: "date", Doc: "the local date at the looked-up place"},
		{Name: "time", Doc: "the local clock time at the looked-up place"},
		{Name: "timezone", Doc: "the looked-up place's timezone name or offset"},
		{Name: "place", Doc: "the normalized place name the lookup resolved"},
		{Name: "user", Doc: "the invoking chatter's display name"},
	}},

	"queue.join": {Entries: withCommon([]module.SpecEntry{
		{Name: "pos", Doc: "the chatter's new position in the queue"},
		{Name: "user", Doc: "the invoking chatter's login"},
	})},
	"queue.next": {Entries: withCommon([]module.SpecEntry{
		{Name: "count", Doc: "how many entries remain in the queue"},
		{Name: "target", Doc: "the login pulled off the front of the queue"},
	})},

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

	"mcsr.record": {Entries: namesOnly(mcsrRecordTokens.Names())},
	"mcsr.lb":     {Entries: namesOnly(mcsrLbTokens.Names())},
	"mcsr.race":   {Entries: namesOnly(mcsrRaceTokens.Names())},

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

	"mcsr.pace":    {Entries: namesOnly(mcsrPaceTokens.Names())},
	"mcsr.nethers": {Entries: namesOnly(mcsrNethersTokens.Names())},

	"fortnite.stats": {Entries: namesOnly(fortniteStatsTokens().Names())},

	"fortnite.store": {Entries: []module.SpecEntry{
		{Name: "count"}, {Name: "date"}, {Name: "items"},
	}},
}

func namesOnly(names []string) []module.SpecEntry {
	out := make([]module.SpecEntry, len(names))
	for i, name := range names {
		out[i] = module.SpecEntry{Name: name}
	}
	return out
}

var commonEntries = []module.SpecEntry{
	{Name: "user", Doc: "the invoking chatter's login"},
	{Name: "channel", Doc: "the broadcaster's login"},
}

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
