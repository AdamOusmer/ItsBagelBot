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

// Welcome ports app/dingress/internal/community/welcome.go: autorole,
// the welcome embed, the goodbye line, and the join/leave log lines. Both
// GUILD_MEMBER_ADD and GUILD_MEMBER_REMOVE ride discord.ingress.event.member
// (see internal/domain/discord's Event doc), distinguished here by
// Event.Type exactly as community's communityEvents map did.
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
	if c.Config.MemberRoleID == "" {
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

// logEntry is one #logs line. Shared by welcome.go and message.go, matching
// community's welcome.go/message.go split before this move.
type logEntry struct {
	Title string
	Body  string
}

// logLine emits a TypePostEmbed Command into the guild's log channel, gated
// on LogsOn and a configured channel, exactly like community's
// Bot.logLine.
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
