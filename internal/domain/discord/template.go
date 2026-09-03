// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

// BotPermissions is the invite integer: kick, ban, manage channels, add
// reactions, view, send, manage messages, embed, attach, history, connect,
// move members, manage roles, slash commands, timeout. No Administrator.
const BotPermissions = 2 | 4 | 16 | 64 | 1024 | 2048 | 8192 | 16384 | 32768 | 65536 | 1048576 | 16777216 | 268435456 | 2147483648 | 1<<40

// Channel type values Discord's REST API uses.
const (
	ChannelText       = 0
	ChannelVoice      = 2
	ChannelCategory   = 4
	ChannelNews       = 5
	ChannelStageVoice = 13
)

// RoleSpec is one role the fill creates (or matches by name).
type RoleSpec struct {
	Name        string
	Hoist       bool
	Mentionable bool
}

// ChannelSpec is one channel or category the fill creates (or matches by name).
type ChannelSpec struct {
	Name     string
	Type     int
	Parent   string // category name; empty for a category itself
	Topic    string
	NSFW     bool
	Staff    bool   // @everyone denied view
	ReadOnly bool   // @everyone can read, not send
	Bind     string // Config field this snowflake fills: live, clips, welcome, voice
}

// CommunityRoles is the streamer-ready role set. Live/Mods/Regulars/Member
// sit below the bot role so Bagel can grant them.
func CommunityRoles() []RoleSpec {
	return []RoleSpec{
		{Name: "Live", Hoist: true, Mentionable: false},
		{Name: "Mods", Hoist: true, Mentionable: true},
		{Name: "Regulars", Hoist: false, Mentionable: false},
		{Name: "Member", Hoist: false, Mentionable: false},
	}
}

// CommunityChannels is the one layout a 1-click setup produces.
func CommunityChannels() []ChannelSpec {
	return []ChannelSpec{
		{Name: "Welcome", Type: ChannelCategory},
		{Name: "welcome", Type: ChannelText, Parent: "Welcome", Topic: "Say hi. Complete onboarding to get Member.", ReadOnly: true, Bind: "welcome"},
		{Name: "rules", Type: ChannelText, Parent: "Welcome", Topic: "House rules.", ReadOnly: true},

		{Name: "Announcements", Type: ChannelCategory},
		{Name: "now-live", Type: ChannelText, Parent: "Announcements", Topic: "Go-live posts. Bagel writes here.", ReadOnly: true, Bind: "live"},
		{Name: "clips", Type: ChannelText, Parent: "Announcements", Topic: "Clips from the stream.", ReadOnly: true, Bind: "clips"},
		{Name: "announcements", Type: ChannelText, Parent: "Announcements", Topic: "Server announcements.", ReadOnly: true},

		{Name: "Community", Type: ChannelCategory},
		{Name: "chat", Type: ChannelText, Parent: "Community"},
		{Name: "clips-talk", Type: ChannelText, Parent: "Community"},
		{Name: "support", Type: ChannelText, Parent: "Community", Topic: "Open a ticket. Bagel posts the panel here.", ReadOnly: true, Bind: "tickets"},

		{Name: "Voice", Type: ChannelCategory},
		{Name: "General", Type: ChannelVoice, Parent: "Voice"},
		{Name: "Watchalong", Type: ChannelVoice, Parent: "Voice"},
		{Name: "AFK", Type: ChannelVoice, Parent: "Voice"},
		{Name: "+ Create voice", Type: ChannelVoice, Parent: "Voice", Bind: "voice"},

		{Name: "Tickets", Type: ChannelCategory, Bind: "ticketcat"},

		{Name: "Staff", Type: ChannelCategory, Staff: true},
		{Name: "mods", Type: ChannelText, Parent: "Staff", Staff: true},
		{Name: "logs", Type: ChannelText, Parent: "Staff", Staff: true, Topic: "Joins, leaves, edits, deletes.", Bind: "logs"},
	}
}

// LivingCommunityMinChannels is the floor at which Set up this server
// refuses: a guild that already looks like a home, not an empty Discord
// default. Default Discord is #general plus two voice channels (3). Our
// template is well above this.
const LivingCommunityMinChannels = 8

// VoiceCloneCap is the per-guild ceiling on join-to-create clones so a
// raid cannot mint hundreds of channels.
const VoiceCloneCap = 12

// InviteURL builds the bot-install link. Discord always shows its own
// confirm; we cannot skip that.
func InviteURL(clientID, redirectURI string) string {
	if clientID == "" {
		return ""
	}
	u := "https://discord.com/oauth2/authorize?client_id=" + clientID +
		"&permissions=" + itoa(BotPermissions) +
		"&scope=bot%20applications.commands"
	if redirectURI != "" {
		u += "&redirect_uri=" + redirectURI + "&response_type=code"
	}
	return u
}

// TemplateURL is the official discord.new create-as-user path. Empty code
// means the operator has not published a source-guild template yet.
func TemplateURL(code string) string {
	if code == "" {
		return ""
	}
	return "https://discord.new/" + code
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
