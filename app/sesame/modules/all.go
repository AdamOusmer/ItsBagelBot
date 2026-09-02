// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
)

// All builds every module wired for the service, in registration order. Adding a
// feature is writing its file and adding one line here. Core modules come first
// so their reserved commands win the registry's first-wins de-dup over any named
// module that might declare a clashing trigger.
func All(d engine.Deps) []module.Module {
	return []module.Module{
		Core(d),
		Personality(d),
		Live(d),
		Cmd(d),
		Shoutout(d),
		Alerts(d),
		Clip(d),
		Followage(d),
		Uptime(d),
		Urchin(d),
		Mcsr(d),
		Fortnite(d),
		ClashRoyale(d),
		Valorant(d),
		// Raffle before Queue: both declare !join, and the registry's first-wins
		// de-dup gives the earlier module the standalone spelling. A channel
		// running both features joins raffles with !join and reaches the queue
		// through !queue join / !queue leave.
		Raffle(d),
		Queue(d),
		Quotes(d),
		Automod(d),
		Moderation(d),
		ChannelPoints(d),
		Loyalty(d),
		// The wager games ride the loyalty economy; they register right after
		// it and own no shared triggers with anything above them.
		Gamble(d),
		Duel(d),
		Govee(d),
		Discord(d),
		TimeOfDay(d),
		Triggers(d),
		EmotePlay(d),
		// SongQueue registers last on purpose: its !sr spelling is checked
		// against every earlier module's triggers by the same first-wins
		// de-dup, so a future collision surfaces as this line failing a test,
		// not as a silent takeover of someone else's command.
		SongQueue(d),
	}
}
