// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"

	"ItsBagelBot/app/discord/engine/internal/cmd"
	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func Welcome() module.Module {
	b := module.NewModule("welcome")
	b.On("GUILD_MEMBER_ADD", onMemberAdd)
	b.On("GUILD_MEMBER_REMOVE", onMemberRemove)
	return b.Build()
}

func onMemberAdd(_ context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MemberEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	autorole(c, ev, emit)
	if !shouldWelcome(c.Config) {
		logJoin(c, ev, emit)
		return nil
	}
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.Config.GuildID, c.Config.WelcomeChannelID),
		ddiscord.WelcomeEmbed(ddiscord.WelcomeCard{Display: shown, AvatarURL: decode.AvatarURL(ev.User)})))
	logJoin(c, ev, emit)
	return nil
}

func shouldWelcome(cfg ddiscord.Config) bool {
	if !cfg.WelcomeOn() {
		return false
	}
	return cfg.WelcomeChannelID != ""
}

func autorole(c *module.Context, ev decode.MemberEvent, emit module.Emit) {
	if c.Config.MemberRoleID == "" || !c.Config.AutoRoleOn() {
		return
	}
	emit(cmd.AddRole(cmd.UserTarget(ev.GuildID, ev.User.ID), cmd.RoleID(c.Config.MemberRoleID)))
}

func onMemberRemove(_ context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MemberEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	if shouldGoodbye(c.Config) {
		emit(cmd.PostChat(cmd.ChannelTarget(c.Config.GuildID, c.Config.WelcomeChannelID), ddiscord.GoodbyeContent(ddiscord.Goodbye{Display: shown})))
	}
	return logLine(c, emit, logEntry{Title: "Member left", Body: shown + " (" + ev.User.ID + ")"})
}

func shouldGoodbye(cfg ddiscord.Config) bool {
	if !cfg.GoodbyeOn() {
		return false
	}
	return cfg.WelcomeChannelID != ""
}

func logJoin(c *module.Context, ev decode.MemberEvent, emit module.Emit) {
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	_ = logLine(c, emit, logEntry{Title: "Member joined", Body: shown + " (" + ev.User.ID + ")"})
}

type logEntry struct {
	Title string
	Body  string
}

func logLine(c *module.Context, emit module.Emit, entry logEntry) error {
	if !c.Config.LogsOn() {
		return nil
	}
	if c.Config.LogChannelID == "" {
		return nil
	}
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.Config.GuildID, c.Config.LogChannelID),
		ddiscord.LogEmbed(ddiscord.LogLine{Title: entry.Title, Body: entry.Body})))
	return nil
}
