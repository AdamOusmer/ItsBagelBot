// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
)

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
		CODM(d),
		ClashRoyale(d),
		Valorant(d),
		Raffle(d),
		Queue(d),
		Quotes(d),
		Automod(d),
		Moderation(d),
		ChannelPoints(d),
		Loyalty(d),
		Gamble(d),
		Duel(d),
		Govee(d),
		TimeOfDay(d),
		Triggers(d),
		EmotePlay(d),
		SongQueue(d),
		TrialTemplate(),
	}
}
