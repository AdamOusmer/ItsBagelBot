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
	b := module.NewModule(ddiscord.ModuleWelcome)
	b.On("GUILD_MEMBER_ADD", onMemberAdd)
	return b.Build()
}

func Goodbye() module.Module {
	b := module.NewModule(ddiscord.ModuleGoodbye)
	b.On("GUILD_MEMBER_REMOVE", onMemberRemove)
	return b.Build()
}

func AutoRole() module.Module {
	b := module.NewModule(ddiscord.ModuleAutoRole)
	b.On("GUILD_MEMBER_ADD", onMemberAutoRole)
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
	cfg, err := settingsOf[ddiscord.WelcomeSettings](c)
	if err != nil {
		return err
	}
	if cfg.WelcomeChannelID == "" {
		return nil
	}
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	emit(cmd.PostEmbed(cmd.ChannelTarget(c.GuildID, cfg.WelcomeChannelID),
		ddiscord.WelcomeEmbed(ddiscord.WelcomeCard{Display: shown, AvatarURL: decode.AvatarURL(ev.User)})))
	return nil
}

func onMemberAutoRole(_ context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MemberEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	cfg, err := settingsOf[ddiscord.AutoRoleSettings](c)
	if err != nil {
		return err
	}
	if cfg.MemberRoleID == "" {
		return nil
	}
	emit(cmd.AddRole(cmd.UserTarget(ev.GuildID, ev.User.ID), cmd.RoleID(cfg.MemberRoleID)))
	return nil
}

func onMemberRemove(_ context.Context, c *module.Context, emit module.Emit) error {
	ev, err := decode.Decode[decode.MemberEvent](c.Event.Raw)
	if err != nil {
		return err
	}
	if ev.User.Bot {
		return nil
	}
	cfg := peerOf[ddiscord.WelcomeSettings](c, ddiscord.ModuleWelcome)
	if cfg.WelcomeChannelID == "" {
		return nil
	}
	shown := decode.DisplayName(decode.Display{User: ev.User, Nick: ev.Nick})
	emit(cmd.PostChat(cmd.ChannelTarget(c.GuildID, cfg.WelcomeChannelID), ddiscord.GoodbyeContent(ddiscord.Goodbye{Display: shown})))
	return nil
}
