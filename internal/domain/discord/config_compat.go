// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strconv"
	"strings"
)

type LogCategory string

const (
	LogMessages   LogCategory = "messages"
	LogMembers    LogCategory = "members"
	LogVoice      LogCategory = "voice"
	LogModeration LogCategory = "moderation"
	LogChannels   LogCategory = "channels"
	LogRoles      LogCategory = "roles"
	LogServer     LogCategory = "server"
)

const LogIgnoredChannelsMax = 25

const (
	VoiceOwnerToken    = "{owner}"
	VoiceNameMax       = 100
	VoiceUserLimitMax  = 99
	VoicePrivacyOpen   = "open"
	VoicePrivacyLocked = "locked"
	VoicePrivacyHidden = "hidden"
)

func withinVoiceLimit(n int) bool { return n >= 0 && n <= VoiceUserLimitMax }

const TicketOpenLimitDefault = 1

const TicketOpenLimitMax = 5

type TicketPanelSpec struct {
	Title  string
	Body   string
	Color  *int
	Button string
}

func (s TicketPanelSpec) ColorOr(fallback int) int {
	if s.Color == nil {
		return fallback
	}
	return *s.Color
}

const (
	TicketPanelTitleDefault  = "Need help?"
	TicketPanelBodyDefault   = "Open a private ticket with the staff."
	TicketPanelButtonDefault = "Open a ticket"

	TicketPanelTitleMax  = 256
	TicketPanelBodyMax   = 1000
	TicketPanelButtonMax = 40
)

func (s TicketPanelSpec) OrDefaults() TicketPanelSpec {
	s.Title = firstNonEmpty(copyText(s.Title), TicketPanelTitleDefault)
	s.Body = firstNonEmpty(copyText(s.Body), TicketPanelBodyDefault)
	s.Button = firstNonEmpty(copyText(s.Button), TicketPanelButtonDefault)
	if s.Color == nil {
		color := LiveColor
		s.Color = &color
	}
	return s
}

func firstNonEmpty(v, fallback copyText) string {
	if t := strings.TrimSpace(string(v)); t != "" {
		return t
	}
	return string(fallback)
}

func ParseHexColor(s string) (int, bool) {
	t := strings.TrimSpace(s)
	if len(t) != 7 || t[0] != '#' {
		return 0, false
	}
	n, err := strconv.ParseUint(t[1:], 16, 24)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

func splitList(s listText) []string {
	if strings.TrimSpace(string(s)) == "" {
		return nil
	}
	parts := strings.Split(string(s), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func containsName(list []string, needle nameText) bool {
	if needle == "" {
		return false
	}
	for _, v := range list {
		if v == string(needle) {
			return true
		}
	}
	return false
}

func splitCSV(s listText) []string {
	if strings.TrimSpace(string(s)) == "" {
		return nil
	}
	parts := strings.Split(string(s), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.ToLower(strings.TrimSpace(p)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (c Config) LinkGuardOn() bool { return c.LinkGuardEnabled == "on" }

func (c Config) roles() RolesSettings {
	return RolesSettings{
		OwnerRoleID: c.OwnerRoleID, LeadModRoleID: c.LeadModRoleID, ModsRoleID: c.ModsRoleID,
		VIPRoleID: c.VIPRoleID, SubscriberRoleID: c.SubscriberRoleID, RegularsRoleID: c.RegularsRoleID,
		PinnedRoles: c.PinnedRoles,
	}
}

func (c Config) live() LiveSettings {
	return LiveSettings{
		LiveChannelID: c.LiveChannelID, CategoryAllow: c.CategoryAllow,
		CategoryDeny: c.CategoryDeny, TwitchLogin: c.TwitchLogin,
	}
}

func (c Config) logs() LogsSettings {
	return LogsSettings{
		LogChannelID:       c.LogChannelID,
		LogMessagesEnabled: c.LogMessagesEnabled, LogMembersEnabled: c.LogMembersEnabled,
		LogVoiceEnabled: c.LogVoiceEnabled, LogModerationEnabled: c.LogModerationEnabled,
		LogChannelsEnabled: c.LogChannelsEnabled, LogRolesEnabled: c.LogRolesEnabled,
		LogServerEnabled: c.LogServerEnabled, LogIgnoreBots: c.LogIgnoreBots,
		LogMessagesChannelID: c.LogMessagesChannelID, LogMembersChannelID: c.LogMembersChannelID,
		LogVoiceChannelID: c.LogVoiceChannelID, LogModerationChannelID: c.LogModerationChannelID,
		LogIgnoredChannels: c.LogIgnoredChannels,
	}
}

func (c Config) voice() VoiceSettings {
	return VoiceSettings{
		VoiceHubID: c.VoiceHubID, VoiceCategoryID: c.VoiceCategoryID,
		VoiceNameTemplate: c.VoiceNameTemplate, VoiceUserLimit: c.VoiceUserLimit,
		VoicePrivacyMode: c.VoicePrivacyMode,
	}
}

func (c Config) tickets() TicketSettings {
	return TicketSettings{
		TicketChannelID: c.TicketChannelID, TicketCategoryID: c.TicketCategoryID,
		TicketArchiveCategoryID: c.TicketArchiveCategoryID, TicketStaffRoles: c.TicketStaffRoles,
		TicketOpenLimit: c.TicketOpenLimit, TicketTranscriptEnabled: c.TicketTranscriptEnabled,
		TicketLogChannelID: c.TicketLogChannelID, TicketPanelTitle: c.TicketPanelTitle,
		TicketPanelBody: c.TicketPanelBody, TicketPanelColor: c.TicketPanelColor,
		TicketPanelButton: c.TicketPanelButton,
	}
}

func (c Config) TicketTranscriptOn() bool { return c.tickets().TranscriptOn() }

func (c Config) LogCategoryOn(cat LogCategory) bool {
	return alertOn(toggleText(c.LogsEnabled)) && c.logs().CategoryOn(cat)
}

func (c Config) LogChannelFor(cat LogCategory) string { return c.logs().ChannelFor(cat) }

func (c Config) LogIgnoredChannelIDs() []string { return c.logs().IgnoredChannelIDs() }

func (c Config) LogIgnores(channelID string) bool { return c.logs().Ignores(channelID) }

func (c Config) LogIgnoreBotsOn() bool { return c.logs().IgnoreBotsOn() }

func (c Config) VoiceName(owner string) string { return c.voice().Name(owner) }

func (c Config) VoiceCategorySet() bool { return c.voice().CategorySet() }

func (c Config) VoiceLimit() int { return c.voice().Limit() }

func (c Config) VoicePrivacy() string { return c.voice().Privacy() }

func (c Config) TicketArchiveCategory() string { return c.tickets().ArchiveCategory() }

func (c Config) TicketStaffRoleIDs() []string { return c.tickets().StaffRoleIDs(c.roles()) }

func (c Config) TicketOpenLimitN() int { return c.tickets().OpenLimitN() }

func (c Config) TicketLogChannel() string { return c.tickets().LogChannel(c.logs()) }

func (c Config) TicketPanel() TicketPanelSpec { return c.tickets().Panel() }

func (c Config) CategoryAllowed(category string) bool { return c.live().CategoryAllowed(category) }

func (c Config) HasCategoryAllow() bool { return c.live().HasCategoryAllow() }

func (c Config) LinkAllowed(raw string) bool {
	return LinkGuardSettings{LinkAllowList: c.LinkAllowList}.Allowed(raw)
}

func (c Config) StaffRoleIDs() []string { return c.roles().StaffRoleIDs() }
