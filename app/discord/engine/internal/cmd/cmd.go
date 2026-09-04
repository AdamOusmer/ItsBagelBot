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

// Target names where a Command applies: always a guild, and (depending on
// the Command type) a channel and/or a member within it. Every builder in
// this file used to take its own bare (guildID, channelID, userID string)
// triple -- CodeScene flagged the repeated triple itself as Primitive
// Obsession, so it is named once here and threaded through every builder
// instead of re-declared per function.
type Target struct {
	GuildID   string
	ChannelID string
	UserID    string
}

// GuildTarget builds a Target for a Command that only needs the guild
// (identity updates, interaction followups -- those key off the
// interaction token, not a channel or member).
func GuildTarget(guildID string) Target {
	return Target{GuildID: guildID}
}

// ChannelTarget builds a Target for a Command that posts or acts in a
// channel.
func ChannelTarget(guildID, channelID string) Target {
	return Target{GuildID: guildID, ChannelID: channelID}
}

// UserTarget builds a Target for a Command that acts on a member.
func UserTarget(guildID, userID string) Target {
	return Target{GuildID: guildID, UserID: userID}
}

func marshal(v any) []byte {
	raw, err := codec.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

// PostEmbed builds a TypePostEmbed Command.
func PostEmbed(t Target, embed ddiscord.Embed) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostEmbed, GuildID: t.GuildID, ChannelID: t.ChannelID,
		Payload: marshal(ddiscord.EmbedPayload{Embed: embed}),
	}
}

// PostPanel builds a TypePostPanel Command (an embed with buttons).
func PostPanel(t Target, content string, embed ddiscord.Embed, buttons []ddiscord.ButtonSpec) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostPanel, GuildID: t.GuildID, ChannelID: t.ChannelID,
		Payload: marshal(ddiscord.EmbedPayload{Content: content, Embed: embed, Buttons: buttons}),
	}
}

// PostChat builds a TypePostChat Command.
func PostChat(t Target, content string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypePostChat, GuildID: t.GuildID, ChannelID: t.ChannelID,
		Payload: marshal(ddiscord.ChatPayload{Content: content}),
	}
}

// AddRole builds a TypeAddRole Command.
func AddRole(t Target, roleID string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeAddRole, GuildID: t.GuildID, UserID: t.UserID,
		Payload: marshal(ddiscord.RolePayload{RoleID: roleID}),
	}
}

// RemoveRole builds a TypeRemoveRole Command.
func RemoveRole(t Target, roleID string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeRemoveRole, GuildID: t.GuildID, UserID: t.UserID,
		Payload: marshal(ddiscord.RolePayload{RoleID: roleID}),
	}
}

// DeleteMessage builds a TypeDeleteMessage Command (mod lane). reason
// should always be set -- it rides Discord's audit-log header (see
// Command.Reason and TypeDeleteMessage's doc), and an automod deletion
// with no reason reads, to a moderator checking the log, as the bot
// malfunctioning rather than acting on purpose.
func DeleteMessage(t Target, messageID, reason string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeDeleteMessage, GuildID: t.GuildID, ChannelID: t.ChannelID, Reason: reason,
		Payload: marshal(ddiscord.DeletePayload{MessageID: messageID}),
	}
}

// TimeoutMember builds a TypeTimeoutMember Command (mod lane).
func TimeoutMember(t Target, untilISO, reason string) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeTimeoutMember, GuildID: t.GuildID, UserID: t.UserID, Reason: reason,
		Payload: marshal(ddiscord.TimeoutPayload{UntilISO: untilISO}),
	}
}

// KickMember builds a TypeKickMember Command (mod lane).
func KickMember(t Target, reason string) ddiscord.Command {
	return ddiscord.Command{Type: ddiscord.TypeKickMember, GuildID: t.GuildID, UserID: t.UserID, Reason: reason}
}

// BanMember builds a TypeBanMember Command (mod lane).
func BanMember(t Target, reason string) ddiscord.Command {
	return ddiscord.Command{Type: ddiscord.TypeBanMember, GuildID: t.GuildID, UserID: t.UserID, Reason: reason}
}

// Followup builds a TypeInteractionFollowup Command completing a deferred
// interaction with a plain text reply.
func Followup(t Target, token, content string, ephemeral bool) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeInteractionFollowup, GuildID: t.GuildID,
		Payload: marshal(ddiscord.FollowupPayload{InteractionToken: token, Content: content, Ephemeral: ephemeral}),
	}
}

// FollowupEmbed builds a TypeInteractionFollowup Command completing a
// deferred interaction with an embed and optional buttons.
func FollowupEmbed(t Target, token string, embed ddiscord.Embed, buttons []ddiscord.ButtonSpec) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeInteractionFollowup, GuildID: t.GuildID,
		Payload: marshal(ddiscord.FollowupPayload{InteractionToken: token, Embed: &embed, Buttons: buttons}),
	}
}

// SetGuildIdentity builds a TypeSetGuildIdentity Command: the bot's own
// nickname and avatar inside one guild. It carries a two-state identity
// rather than the image, because outgress embeds the avatar and the same
// picture would otherwise ride the lane once per guild (see
// ddiscord.GuildIdentity).
func SetGuildIdentity(t Target, id ddiscord.GuildIdentity) ddiscord.Command {
	return ddiscord.Command{
		Type: ddiscord.TypeSetGuildIdentity, GuildID: t.GuildID,
		Payload: marshal(ddiscord.IdentityPayload{Identity: id}),
	}
}
