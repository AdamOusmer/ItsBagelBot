// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

// DiscordSetupRequest is bagel.rpc.outgress.discord.setup.
type DiscordSetupRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

// DiscordSetupReply is the filled template the dashboard writes into the
// Discord module blob. Refused is set when the guild already looks lived-in.
type DiscordSetupReply struct {
	GuildID          string `json:"guild_id,omitempty"`
	LiveChannelID    string `json:"live_channel_id,omitempty"`
	ClipsChannelID   string `json:"clips_channel_id,omitempty"`
	WelcomeChannelID string `json:"welcome_channel_id,omitempty"`
	AlertsChannelID  string `json:"alerts_channel_id,omitempty"`
	VoiceHubID       string `json:"voice_hub_id,omitempty"`
	LiveRoleID       string `json:"live_role_id,omitempty"`
	ModsRoleID       string `json:"mods_role_id,omitempty"`
	RegularsRoleID   string `json:"regulars_role_id,omitempty"`
	MemberRoleID     string `json:"member_role_id,omitempty"`
	Refused          string `json:"refused,omitempty"`
	Error            string `json:"error,omitempty"`
}

// DiscordPostRequest is bagel.rpc.outgress.discord.post: Bagel's own
// changelog/status channel, or any connected guild channel the operator names.
type DiscordPostRequest struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

type DiscordPostReply struct {
	Error string `json:"error,omitempty"`
}
