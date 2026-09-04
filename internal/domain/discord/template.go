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
	// Feature names an optional tier this role belongs to; see
	// ChannelSpec.Feature.
	Feature string
	// Permissions is the role's Discord permission bitfield. 0 means the role
	// grants nothing on its own, which is correct for every tier that acts
	// through the bot's slash commands rather than through Discord's own UI.
	Permissions int64
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
	ReadOnly bool // @everyone can read, not send
	// AllowRoles gates the channel: @everyone is denied view and only these
	// roles (by template name) are allowed it. Empty means public.
	//
	// This replaced a plain Staff bool once the template grew subscriber and
	// VIP areas. A bool could only express one gated audience, and the second
	// one would have arrived as a second bool, then a third -- with the
	// overwrite logic branching per flag instead of reading the data.
	AllowRoles []string
	// Feature names an optional tier this spec belongs to ("subscribers").
	// Empty means always created. The fill skips specs for a feature the
	// streamer did not enable, so a server without subs never grows a locked
	// category nobody can open.
	Feature string
	Bind    string // Config field this snowflake fills: live, clips, welcome, voice
}

// PermAdministrator is Discord's Administrator bit. It bypasses every channel
// overwrite and every other permission check in the guild, and it cannot be
// narrowed: a role holding it can delete channels, ban anyone below it in the
// hierarchy, and change the server itself.
//
// It is granted to Lead Mod only, deliberately and with the cost understood: a
// compromised Lead Mod account is exactly the nuke scenario the audit-log
// watching exists to catch, and no permission tuning can prevent it once the
// bit is held. The mitigation is the response path (strip the actor's roles
// fast), not the grant.
//
// Mods deliberately holds NOTHING. That tier moderates through the bot's slash
// commands, which check the role themselves, so every action it takes is
// rate-limited, logged with an audit reason, and revocable by turning the bot
// off. Handing Mods raw Discord permissions would move all of that outside
// anything we can see.
const PermAdministrator int64 = 1 << 3

// The Mods permission set: real Discord moderation, deliberately without
// Administrator. Every bit here is one a moderator visibly needs, and the
// absence of Administrator is what keeps the tier bounded -- a Mod cannot
// delete channels, change the server, or grant themselves anything.
//
// Named individually rather than as one opaque number so the next person can
// see what a Mod can do without decoding a bitfield.
const (
	PermKickMembers     int64 = 1 << 1
	PermBanMembers      int64 = 1 << 2
	PermViewAuditLog    int64 = 1 << 7
	PermManageMessages  int64 = 1 << 13
	PermMuteMembers     int64 = 1 << 22
	PermDeafenMembers   int64 = 1 << 23
	PermMoveMembers     int64 = 1 << 24
	PermManageNicknames int64 = 1 << 27
	PermManageThreads   int64 = 1 << 34
	PermModerateMembers int64 = 1 << 40 // timeout
)

// PermModerator is what the Mods role holds. Timeout and message deletion are
// the everyday tools; kick and ban are the escalation; the voice bits matter
// because a raid arrives in voice as often as in text. Audit log is included
// so a mod can see what another mod did without asking a Lead Mod.
const PermModerator = PermKickMembers | PermBanMembers | PermViewAuditLog |
	PermManageMessages | PermMuteMembers | PermDeafenMembers | PermMoveMembers |
	PermManageNicknames | PermManageThreads | PermModerateMembers

// FeatureSubscribers gates the Subscriber role and its locked category. See
// ChannelSpec.Feature.
const FeatureSubscribers = "subscribers"

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
	// RoleColorVIP is silver and RoleColorSubscriber gold, matching the tier
	// language the dashboard already uses (free green, paid gold, vip silver).
	// Silver reading as "lower" than gold is a known quirk of that palette,
	// kept because one vocabulary across the product beats a prettier one
	// that means something different in each place.
	RoleColorVIP        = 0xC0C4CC
	RoleColorSubscriber = 0xD4A340
	// RoleColorRegulars is a muted green: present, friendly, not staff.
	RoleColorRegulars = 0x5FA85F
)

// Role names the template creates. Referenced by ChannelSpec.AllowRoles, so
// they are constants rather than repeated literals: a typo in a gate would
// otherwise silently produce a channel nobody can see.
const (
	RoleOwner      = "Owner"
	RoleLeadMod    = "Lead Mod"
	RoleMods       = "Mods"
	RoleVIP        = "VIP"
	RoleSubscriber = "Subscriber"
	RoleRegulars   = "Regulars"
	RoleMember     = "Member"
)

// StaffRoles is the audience for staff-only channels.
var StaffRoles = []string{RoleOwner, RoleLeadMod, RoleMods}

// SubscriberRoles is the audience for the subscriber area: subs, plus the
// tiers above them. A VIP losing access to sub channels because they are not
// also subscribed reads as a bug to the person it happens to.
var SubscriberRoles = []string{RoleOwner, RoleLeadMod, RoleMods, RoleVIP, RoleSubscriber}

// VIPRoles is the audience for the VIP area. Subscribers are NOT included:
// the whole point of a VIP room is that it is smaller than the sub room.
var VIPRoles = []string{RoleOwner, RoleLeadMod, RoleMods, RoleVIP}

// CommunityRoles is the streamer-ready role set, in descending authority.
// Every one of them is created below the bot's own role so Bagel can still
// grant and revoke them; a role above the bot is untouchable to it.
//
// Order matters twice over. Discord positions newly created roles from the
// bottom, and it renders a member in their highest COLOURED role, so this
// slice is also the visual hierarchy.
func CommunityRoles() []RoleSpec {
	return []RoleSpec{
		// The streamer. Hoisted to the top of the member list, not
		// mentionable: pinging the owner should be deliberate, not an
		// @-autocomplete away for everyone in the server.
		{Name: RoleOwner, Hoist: true, Mentionable: false, Color: RoleColorOwner},
		// The admin tier. Mentionable, because reaching them quickly is the
		// entire point of the tier existing.
		{Name: RoleLeadMod, Hoist: true, Mentionable: true, Color: RoleColorLeadMod, Permissions: PermAdministrator},
		// Real moderation powers, bounded: see PermModerator.
		{Name: RoleMods, Hoist: true, Mentionable: true, Color: RoleColorMods, Permissions: PermModerator},
		// VIP and Subscriber are access tiers, not power tiers: they hold no
		// permissions and exist to be named in channel gates. Both hoisted,
		// because being visibly in them is the reward.
		{Name: RoleVIP, Hoist: true, Mentionable: false, Color: RoleColorVIP},
		{Name: RoleSubscriber, Hoist: true, Mentionable: false, Color: RoleColorSubscriber, Feature: FeatureSubscribers},
		// Regulars and Member are not hoisted. Everyone ends up in Member, so
		// hoisting it would put the entire server in one giant member-list
		// group and destroy the separation the tiers above exist to create.
		{Name: RoleRegulars, Hoist: false, Mentionable: false, Color: RoleColorRegulars},
		// No colour: see RoleSpec.Color. Member is held by everyone, so a
		// colour would win the name colour for anyone whose only other roles
		// are uncoloured.
		{Name: RoleMember, Hoist: false, Mentionable: false},
	}
}

// CommunityChannels is the one layout a 1-click setup produces.
func CommunityChannels() []ChannelSpec {
	return []ChannelSpec{
		{Name: "Welcome", Type: ChannelCategory},
		{Name: "welcome", Type: ChannelText, Parent: "Welcome", Topic: "Say hi. Complete onboarding to get Member.", ReadOnly: true, Bind: "welcome"},
		{Name: "rules", Type: ChannelText, Parent: "Welcome", Topic: "House rules.", ReadOnly: true},
		{Name: "roles", Type: ChannelText, Parent: "Welcome", Topic: "What each role means and how to get it.", ReadOnly: true},

		{Name: "Announcements", Type: ChannelCategory},
		{Name: "now-live", Type: ChannelText, Parent: "Announcements", Topic: "Go-live posts. Bagel writes here.", ReadOnly: true, Bind: "live"},
		{Name: "clips", Type: ChannelText, Parent: "Announcements", Topic: "Clips from the stream.", ReadOnly: true, Bind: "clips"},
		{Name: "announcements", Type: ChannelText, Parent: "Announcements", Topic: "Server announcements.", ReadOnly: true},

		{Name: "Community", Type: ChannelCategory},
		{Name: "chat", Type: ChannelText, Parent: "Community"},
		{Name: "clips-talk", Type: ChannelText, Parent: "Community"},
		{Name: "media", Type: ChannelText, Parent: "Community", Topic: "Images and clips from anywhere."},
		{Name: "off-topic", Type: ChannelText, Parent: "Community"},
		{Name: "support", Type: ChannelText, Parent: "Community", Topic: "Open a ticket. Bagel posts the panel here.", ReadOnly: true, Bind: "tickets"},

		// Subscriber area. Gated on the Subscriber role and everything above
		// it; created only when the streamer enables the subscriber tier (see
		// Config.SubscribersOn), because an empty locked category in a server
		// that has no subs is furniture nobody can open.
		{Name: "Subscribers", Type: ChannelCategory, AllowRoles: SubscriberRoles, Feature: FeatureSubscribers, Bind: "subcat"},
		{Name: "sub-chat", Type: ChannelText, Parent: "Subscribers", AllowRoles: SubscriberRoles, Feature: FeatureSubscribers, Topic: "Subscriber-only chat.", Bind: "subs"},
		{Name: "sub-media", Type: ChannelText, Parent: "Subscribers", AllowRoles: SubscriberRoles, Feature: FeatureSubscribers, Topic: "Subscriber-only media."},

		// VIP area. Deliberately excludes Subscriber: a VIP room that every
		// sub can read is not a VIP room.
		{Name: "VIP", Type: ChannelCategory, AllowRoles: VIPRoles, Bind: "vipcat"},
		{Name: "vip-lounge", Type: ChannelText, Parent: "VIP", AllowRoles: VIPRoles, Topic: "VIP chat.", Bind: "vip"},

		{Name: "Voice", Type: ChannelCategory},
		{Name: "General", Type: ChannelVoice, Parent: "Voice"},
		{Name: "Watchalong", Type: ChannelVoice, Parent: "Voice"},
		{Name: "AFK", Type: ChannelVoice, Parent: "Voice"},
		{Name: "+ Create voice", Type: ChannelVoice, Parent: "Voice", Bind: "voice"},

		{Name: "Tickets", Type: ChannelCategory, Bind: "ticketcat"},

		{Name: "Staff", Type: ChannelCategory, AllowRoles: StaffRoles},
		{Name: "mods", Type: ChannelText, Parent: "Staff", AllowRoles: StaffRoles},
		{Name: "logs", Type: ChannelText, Parent: "Staff", AllowRoles: StaffRoles, Topic: "Joins, leaves, edits, deletes.", Bind: "logs"},
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

// FeatureEnabled reports whether a spec's feature gate is satisfied. An
// ungated spec is always created; a gated one only when that feature is on.
func FeatureEnabled(feature string, subscribers bool) bool {
	switch feature {
	case "":
		return true
	case FeatureSubscribers:
		return subscribers
	default:
		// An unknown gate is treated as OFF rather than ON: a typo in a new
		// spec's Feature should leave the channel uncreated, which is
		// noticeable, rather than create it ungated, which is not.
		return false
	}
}
