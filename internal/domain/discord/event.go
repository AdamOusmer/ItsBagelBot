// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

type Event struct {
	Type             string `json:"type"`
	GuildID          string `json:"guild_id"`
	ChannelID        string `json:"channel_id,omitempty"`
	UserID           string `json:"user_id,omitempty"`
	Raw              []byte `json:"raw"`
	ReceivedAtUnixMs int64  `json:"received_at_unix_ms"`
}

// Every subject must also be listed in bus.DiscordIngressStream.Subjects.
const (
	SubjectEventMessage     = "discord.ingress.event.message"
	SubjectEventMember      = "discord.ingress.event.member"
	SubjectEventVoice       = "discord.ingress.event.voice"
	SubjectEventInteraction = "discord.ingress.event.interaction"
	SubjectEventAudit       = "discord.ingress.event.audit"
	SubjectEventGuild       = "discord.ingress.event.guild"
)
