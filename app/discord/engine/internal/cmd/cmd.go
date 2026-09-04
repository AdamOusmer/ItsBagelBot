// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package cmd builds internal/domain/discord.Command values, centralizing
// the Payload marshalling every module would otherwise repeat. A marshal
// failure is swallowed into an empty Payload rather than propagated: a
// Command with a body outgress cannot decode just no-ops that one send, the
// same severity as any other single dropped post, and a builder that can
// fail would force every module Handler to thread an error return through
// what is otherwise a one-line Emit call.
package cmd

import (
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

func marshal(v any) []byte {
	raw, err := codec.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

// PostEmbed builds a TypePostEmbed Command.
func PostEmbed(guildID, channelID string, embed ddiscord.Embed) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostEmbed, GuildID: guildID, ChannelID: channelID,
		Payload: marshal(ddiscord.EmbedPayload{Embed: embed}),
	}
}

// PostPanel builds a TypePostPanel Command (an embed with buttons).
func PostPanel(guildID, channelID, content string, embed ddiscord.Embed, buttons []ddiscord.ButtonSpec) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostPanel, GuildID: guildID, ChannelID: channelID,
		Payload: marshal(ddiscord.EmbedPayload{Content: content, Embed: embed, Buttons: buttons}),
	}
}

// PostChat builds a TypePostChat Command.
func PostChat(guildID, channelID, content string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostChat, GuildID: guildID, ChannelID: channelID,
		Payload: marshal(ddiscord.ChatPayload{Content: content}),
	}
}

// AddRole builds a TypeAddRole Command.
func AddRole(guildID, userID, roleID string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeAddRole, GuildID: guildID, UserID: userID,
		Payload: marshal(ddiscord.RolePayload{RoleID: roleID}),
	}
}

// RemoveRole builds a TypeRemoveRole Command.
func RemoveRole(guildID, userID, roleID string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeRemoveRole, GuildID: guildID, UserID: userID,
		Payload: marshal(ddiscord.RolePayload{RoleID: roleID}),
	}
}

// DeleteMessage builds a TypeDeleteMessage Command (mod lane). reason
// should always be set -- it rides Discord's audit-log header (see
// Command.Reason and TypeDeleteMessage's doc), and an automod deletion
// with no reason reads, to a moderator checking the log, as the bot
// malfunctioning rather than acting on purpose.
func DeleteMessage(guildID, channelID, messageID, reason string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeDeleteMessage, GuildID: guildID, ChannelID: channelID, Reason: reason,
		Payload: marshal(ddiscord.DeletePayload{MessageID: messageID}),
	}
}

// TimeoutMember builds a TypeTimeoutMember Command (mod lane).
func TimeoutMember(guildID, userID, untilISO, reason string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeTimeoutMember, GuildID: guildID, UserID: userID, Reason: reason,
		Payload: marshal(ddiscord.TimeoutPayload{UntilISO: untilISO}),
	}
}

// KickMember builds a TypeKickMember Command (mod lane).
func KickMember(guildID, userID, reason string) ddiscord.Command {
	return ddiscord.Command{Type: ddiscord.TypeKickMember, GuildID: guildID, UserID: userID, Reason: reason}
}

// BanMember builds a TypeBanMember Command (mod lane).
func BanMember(guildID, userID, reason string) ddiscord.Command {
	return ddiscord.Command{Type: ddiscord.TypeBanMember, GuildID: guildID, UserID: userID, Reason: reason}
}

// Followup builds a TypeInteractionFollowup Command completing a deferred
// interaction with a plain text reply.
func Followup(guildID, token, content string, ephemeral bool) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeInteractionFollowup, GuildID: guildID,
		Payload: marshal(ddiscord.FollowupPayload{InteractionToken: token, Content: content, Ephemeral: ephemeral}),
	}
}

// FollowupEmbed builds a TypeInteractionFollowup Command completing a
// deferred interaction with an embed and optional buttons.
func FollowupEmbed(guildID, token string, embed ddiscord.Embed, buttons []ddiscord.ButtonSpec) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeInteractionFollowup, GuildID: guildID,
		Payload: marshal(ddiscord.FollowupPayload{InteractionToken: token, Embed: &embed, Buttons: buttons}),
	}
}

// SetGuildIdentity builds a TypeSetGuildIdentity Command: the bot's own
// nickname and avatar inside one guild. It carries a two-state identity
// rather than the image, because outgress embeds the avatar and the same
// picture would otherwise ride the lane once per guild (see
// ddiscord.GuildIdentity).
func SetGuildIdentity(guildID string, id ddiscord.GuildIdentity) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: guildID,
		Payload: marshal(ddiscord.IdentityPayload{Identity: id}),
	}
}
