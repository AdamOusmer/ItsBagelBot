// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

// BotPermissions is the invite integer: kick, ban, manage channels, add
// reactions, view, send, manage messages, embed, attach, history, connect,
// move members, manage roles, slash commands, timeout, change nickname. No
// Administrator.
//
// CHANGE_NICKNAME (1<<26) is what lets the bot rename ITSELF per guild, which
// is how a premium streamer's server shows "ItsBagelBot - Premium". Without
// it the avatar half of that still works (Modify Current Member needs no
// permission for avatar or banner) and only the name silently 403s, which
// reads as a half-broken feature rather than a missing one.
//
// Discord freezes these into the bot's role AT INSTALL. Raising this integer
// only affects new installs; a guild that already has the bot keeps its old
// role until it re-authorizes or an admin ticks the box by hand. Treat a
// bumped value here as a migration, never as a rollout.
//
// The dashboard carries this same number as a decimal literal
// (console/dashboard/src/lib/server/discord-oauth.ts) because it builds the
// invite URL in TypeScript. permissions.test.ts recomputes this expression
// from source and fails if the two drift.
const BotPermissions = 2 | 4 | 16 | 64 | 1024 | 2048 | 8192 | 16384 | 32768 | 65536 | 1048576 | 16777216 | 67108864 | 268435456 | 2147483648 | 1<<40

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
	// Color is the role colour as a Discord RGB integer. 0 means "no colour",
	// which Discord renders as the default grey and, unlike a real colour,
	// does not win the member-list name colour from a lower role. Member is
	// deliberately 0 for that reason: everyone has it, so colouring it would
	// override every other role for anyone who has nothing else.
	Color int
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

// Role colours. Distinct hues rather than shades so the member list is
// readable at a glance: Discord shows a member in the colour of their highest
// COLOURED role, so two roles a shade apart are indistinguishable in the one
// place the colour actually appears.
const (
	// RoleColorOwner is the brand amber, matching the embed accent, because
	// the owner is the channel the whole server belongs to.
	RoleColorOwner = 0xC47A3A
	// RoleColorLeadMod is a deep red: the escalation tier, and the one
	// people need to find fast when something is going wrong.
	RoleColorLeadMod = 0xC0392B
	// RoleColorMods is a cool teal, clearly not the red above it, so the two
	// staff tiers do not read as one block.
	RoleColorMods = 0x3B8EA5
	// RoleColorRegulars is a muted green: present, friendly, not staff.
	RoleColorRegulars = 0x5FA85F
)

// CommunityRoles is the streamer-ready role set, in descending authority.
// Every one of them is created below the bot's own role so Bagel can still
// grant and revoke them; a role above the bot is untouchable to it.
//
// Order matters twice over. Discord positions newly created roles from the
// bottom, and it renders a member in their highest COLOURED role, so this
// slice is also the visual hierarchy.
func CommunityRoles() []RoleSpec {
	return []RoleSpec{
		// The streamer. Hoisted so it sits at the top of the member list,
		// not mentionable because pinging the owner should be a deliberate
		// act, not an @-autocomplete away for everyone in the server.
		{Name: "Owner", Hoist: true, Mentionable: false, Color: RoleColorOwner},
		// Lead Mod is the admin tier: the people who action other mods and
		// hold the destructive permissions. Mentionable, because reaching
		// them quickly is the entire point of the tier existing.
		{Name: "Lead Mod", Hoist: true, Mentionable: true, Color: RoleColorLeadMod},
		{Name: "Mods", Hoist: true, Mentionable: true, Color: RoleColorMods},
		{Name: "Regulars", Hoist: false, Mentionable: false, Color: RoleColorRegulars},
		// No colour: see RoleSpec.Color. Member is held by everyone, so a
		// colour here would override every other role for anyone whose only
		// other roles are uncoloured.
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
