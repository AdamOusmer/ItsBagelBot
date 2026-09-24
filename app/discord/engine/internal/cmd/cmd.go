// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cmd

import (
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

type Target struct {
	GuildID   string
	ChannelID string
	UserID    string
}

func GuildTarget(guildID string) Target {
	return Target{GuildID: guildID}
}

func ChannelTarget(guildID, channelID string) Target {
	return Target{GuildID: guildID, ChannelID: channelID}
}

func UserTarget(guildID, userID string) Target {
	return Target{GuildID: guildID, UserID: userID}
}

type Reason string

type Token string

type RoleID string

func marshal(v any) []byte {
	raw, err := codec.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func PostEmbed(t Target, embed ddiscord.Embed) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostEmbed, GuildID: t.GuildID, ChannelID: t.ChannelID,
		Payload: marshal(ddiscord.EmbedPayload{Embed: embed}),
	}
}

func PostPanel(t Target, content string, embed ddiscord.Embed, buttons []ddiscord.ButtonSpec) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostPanel, GuildID: t.GuildID, ChannelID: t.ChannelID,
		Payload: marshal(ddiscord.EmbedPayload{Content: content, Embed: embed, Buttons: buttons}),
	}
}

func PostChat(t Target, content string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostChat, GuildID: t.GuildID, ChannelID: t.ChannelID,
		Payload: marshal(ddiscord.ChatPayload{Content: content}),
	}
}

func AddRole(t Target, roleID RoleID) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeAddRole, GuildID: t.GuildID, UserID: t.UserID,
		Payload: marshal(ddiscord.RolePayload{RoleID: string(roleID)}),
	}
}

func RemoveRole(t Target, roleID RoleID) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeRemoveRole, GuildID: t.GuildID, UserID: t.UserID,
		Payload: marshal(ddiscord.RolePayload{RoleID: string(roleID)}),
	}
}

func DeleteMessage(t Target, messageID string, reason Reason) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeDeleteMessage, GuildID: t.GuildID, ChannelID: t.ChannelID, Reason: string(reason),
		Payload: marshal(ddiscord.DeletePayload{MessageID: messageID}),
	}
}

func TimeoutMember(t Target, untilISO string, reason Reason) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeTimeoutMember, GuildID: t.GuildID, UserID: t.UserID, Reason: string(reason),
		Payload: marshal(ddiscord.TimeoutPayload{UntilISO: untilISO}),
	}
}

func KickMember(t Target, reason Reason) ddiscord.Command {
	return ddiscord.Command{Type: ddiscord.TypeKickMember, GuildID: t.GuildID, UserID: t.UserID, Reason: string(reason)}
}

func BanMember(t Target, reason Reason) ddiscord.Command {
	return ddiscord.Command{Type: ddiscord.TypeBanMember, GuildID: t.GuildID, UserID: t.UserID, Reason: string(reason)}
}

func Followup(t Target, token Token, content string, ephemeral bool) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeInteractionFollowup, GuildID: t.GuildID,
		Payload: marshal(ddiscord.FollowupPayload{InteractionToken: string(token), Content: content, Ephemeral: ephemeral}),
	}
}

func FollowupEmbed(t Target, token Token, embed ddiscord.Embed, buttons []ddiscord.ButtonSpec) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeInteractionFollowup, GuildID: t.GuildID,
		Payload: marshal(ddiscord.FollowupPayload{InteractionToken: string(token), Embed: &embed, Buttons: buttons}),
	}
}

func SetGuildIdentity(t Target, id ddiscord.GuildIdentity) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: t.GuildID,
		Payload: marshal(ddiscord.IdentityPayload{Identity: id}),
	}
}

func StripRoles(t Target, reason Reason) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeStripRoles, GuildID: t.GuildID, UserID: t.UserID, Reason: string(reason),
	}
}

func Lockdown(t Target, everyoneRoleID string, categoryIDs []string, reason Reason) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeLockdown, GuildID: t.GuildID, Reason: string(reason),
		Payload: marshal(ddiscord.LockdownPayload{EveryoneRoleID: everyoneRoleID, CategoryIDs: categoryIDs}),
	}
}

func Unlock(t Target, reason Reason) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeUnlock, GuildID: t.GuildID, Reason: string(reason),
	}
}
