// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

const BotPermissions = 2 | 4 | 16 | 64 | 1024 | 2048 | 8192 | 16384 | 32768 | 65536 | 1048576 | 16777216 | 67108864 | 268435456 | 2147483648 | 1<<40

const (
	ChannelText       = 0
	ChannelVoice      = 2
	ChannelCategory   = 4
	ChannelNews       = 5
	ChannelStageVoice = 13
	ChannelForum      = 15
)

type RoleSpec struct {
	Name        string
	Hoist       bool
	Mentionable bool
	Feature     string
	Permissions int64
	Color       int
}

type ChannelSpec struct {
	Name       string
	Type       int
	Parent     string
	Topic      string
	NSFW       bool
	ReadOnly   bool
	AllowRoles []string
	Feature    string
	Bind       string
}

const PermAdministrator int64 = 1 << 3

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
	PermModerateMembers int64 = 1 << 40
)

const PermModerator = PermKickMembers | PermBanMembers | PermViewAuditLog |
	PermManageMessages | PermMuteMembers | PermDeafenMembers | PermMoveMembers |
	PermManageNicknames | PermManageThreads | PermModerateMembers

const FeatureSubscribers = "subscribers"

const (
	RoleColorOwner      = 0xC47A3A
	RoleColorLeadMod    = 0xC0392B
	RoleColorMods       = 0x3B8EA5
	RoleColorVIP        = 0xC0C4CC
	RoleColorSubscriber = 0xD4A340
	RoleColorRegulars   = 0x5FA85F
)

const (
	RoleOwner      = "Owner"
	RoleLeadMod    = "Lead Mod"
	RoleMods       = "Mods"
	RoleVIP        = "VIP"
	RoleSubscriber = "Subscriber"
	RoleRegulars   = "Regulars"
	RoleMember     = "Member"
)

var StaffRoles = []string{RoleOwner, RoleLeadMod, RoleMods}

var SubscriberRoles = []string{RoleOwner, RoleLeadMod, RoleMods, RoleVIP, RoleSubscriber}

var VIPRoles = []string{RoleOwner, RoleLeadMod, RoleMods, RoleVIP}

func CommunityRoles() []RoleSpec {
	return []RoleSpec{
		{Name: RoleOwner, Hoist: true, Mentionable: false, Color: RoleColorOwner},
		{Name: RoleLeadMod, Hoist: true, Mentionable: true, Color: RoleColorLeadMod, Permissions: PermAdministrator},
		{Name: RoleMods, Hoist: true, Mentionable: true, Color: RoleColorMods, Permissions: PermModerator},
		{Name: RoleVIP, Hoist: true, Mentionable: false, Color: RoleColorVIP},
		{Name: RoleSubscriber, Hoist: true, Mentionable: false, Color: RoleColorSubscriber, Feature: FeatureSubscribers},
		{Name: RoleRegulars, Hoist: false, Mentionable: false, Color: RoleColorRegulars},
		{Name: RoleMember, Hoist: false, Mentionable: false},
	}
}

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

		{Name: "Subscribers", Type: ChannelCategory, AllowRoles: SubscriberRoles, Feature: FeatureSubscribers, Bind: "subcat"},
		{Name: "sub-chat", Type: ChannelText, Parent: "Subscribers", AllowRoles: SubscriberRoles, Feature: FeatureSubscribers, Topic: "Subscriber-only chat.", Bind: "subs"},
		{Name: "sub-media", Type: ChannelText, Parent: "Subscribers", AllowRoles: SubscriberRoles, Feature: FeatureSubscribers, Topic: "Subscriber-only media."},

		{Name: "VIP", Type: ChannelCategory, AllowRoles: VIPRoles, Bind: "vipcat"},
		{Name: "vip-lounge", Type: ChannelText, Parent: "VIP", AllowRoles: VIPRoles, Topic: "VIP chat.", Bind: "vip"},

		{Name: "Voice", Type: ChannelCategory},
		{Name: "General", Type: ChannelVoice, Parent: "Voice"},
		{Name: "Watchalong", Type: ChannelVoice, Parent: "Voice"},
		{Name: "AFK", Type: ChannelVoice, Parent: "Voice"},
		{Name: "+ Create voice", Type: ChannelVoice, Parent: "Voice", Bind: "voice"},

		{Name: "Tickets", Type: ChannelCategory, Bind: "ticketcat"},
		{Name: "Archive", Type: ChannelCategory, AllowRoles: StaffRoles, ReadOnly: true, Bind: "ticketarchive"},

		{Name: "Staff", Type: ChannelCategory, AllowRoles: StaffRoles},
		{Name: "mods", Type: ChannelText, Parent: "Staff", AllowRoles: StaffRoles},
		{Name: "logs", Type: ChannelText, Parent: "Staff", AllowRoles: StaffRoles, Topic: "Joins, leaves, edits, deletes.", Bind: "logs"},
	}
}

const LivingCommunityMinChannels = 8

const VoiceCloneCap = 12

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

func FeatureEnabled(feature string, subscribers bool) bool {
	switch feature {
	case "":
		return true
	case FeatureSubscribers:
		return subscribers
	default:
		return false
	}
}
