// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

func TrialTemplate() module.Module {
	m := module.NewModule("trial", module.KindCore).Trial()

	m.Command("ign").Run(broadcasterLine("%[1]s's in-game name is %[1]s. Add them to squad up."))
	m.Command("event").Run(chatLine("No community event is running right now. Watch !discord for the next one."))
	m.Command("discord").Run(loginLine("Join the community Discord: discord.gg/%s"))
	m.Command("drop").Aliases("drops").Run(chatLine("Drops are on while the stream is tagged Drops Enabled. Link your game account at twitch.tv/drops/campaigns and claim rewards from your inventory."))
	m.Command("cape").Run(chatLine("The cape is a Drops reward: watch with Drops enabled, then claim it from your Twitch inventory. See !drop."))
	m.Command("socials").Run(loginLine("Socials: x.com/%[1]s · youtube.com/@%[1]s · tiktok.com/@%[1]s · instagram.com/%[1]s"))
	m.Command("twitter").Aliases("x").Run(loginLine("x.com/%s"))
	m.Command("youtube").Aliases("yt").Run(loginLine("youtube.com/@%s"))
	m.Command("tiktok").Run(loginLine("tiktok.com/@%s"))
	m.Command("instagram").Aliases("ig").Run(loginLine("instagram.com/%s"))
	m.Command("schedule").Run(loginLine("Stream schedule: twitch.tv/%s/schedule"))
	m.Command("merch").Run(chatLine("Merch store link is in the panels below the stream."))
	m.Command("sens").Aliases("settings", "crosshair").Run(chatLine("Sensitivity, crosshair and game settings are in the panels below the stream."))
	m.Command("specs").Aliases("setup", "pc").Run(chatLine("PC specs and setup are in the panels below the stream."))
	m.Command("prime").Aliases("sub").Run(broadcasterLine("Link Amazon Prime at twitch.tv/prime and sub to %s for free every month."))
	m.Command("lurk").Run(chatterLine("%s is lurking. Enjoy the stream!"))
	m.Command("unlurk").Run(chatterLine("Welcome back, %s!"))

	return m.Build()
}

func broadcasterLine(format string) module.RunFunc {
	return textLine(func(c *module.Context) string {
		return fmt.Sprintf(format, c.Env.BroadcasterName())
	})
}

func loginLine(format string) module.RunFunc {
	return textLine(func(c *module.Context) string {
		return fmt.Sprintf(format, c.Env.BroadcasterUserLogin)
	})
}

func chatterLine(format string) module.RunFunc {
	return textLine(func(c *module.Context) string { return fmt.Sprintf(format, c.Env.ChatterName()) })
}

func textLine(text func(*module.Context) string) module.RunFunc {
	return func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: text(c)})
		return nil
	}
}
