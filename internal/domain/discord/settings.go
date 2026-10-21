// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"slices"
	"strconv"
	"strings"
)

type RolesSettings struct {
	OwnerRoleID      string `json:"ownerRoleId"`
	LeadModRoleID    string `json:"leadModRoleId"`
	ModsRoleID       string `json:"modsRoleId"`
	VIPRoleID        string `json:"vipRoleId"`
	SubscriberRoleID string `json:"subscriberRoleId"`
	RegularsRoleID   string `json:"regularsRoleId"`
	PinnedRoles      string `json:"pinnedRoles"`
}

type LiveSettings struct {
	LiveChannelID string `json:"liveChannelId"`
	CategoryAllow string `json:"categoryAllow"`
	CategoryDeny  string `json:"categoryDeny"`
	TwitchLogin   string `json:"twitchLogin"`
}

type ClipsSettings struct {
	ClipsChannelID string `json:"clipsChannelId"`
}

type WelcomeSettings struct {
	WelcomeChannelID string `json:"welcomeChannelId"`
}

type GoodbyeSettings struct{}

type AutoRoleSettings struct {
	MemberRoleID string `json:"memberRoleId"`
}

type SubscribersSettings struct {
	SubsChannelID  string `json:"subsChannelId"`
	SubsCategoryID string `json:"subsCategoryId"`
	VIPChannelID   string `json:"vipChannelId"`
	VIPCategoryID  string `json:"vipCategoryId"`
}

type VoiceSettings struct {
	VoiceHubID        string `json:"voiceHubId"`
	VoiceCategoryID   string `json:"voiceCategoryId"`
	VoiceNameTemplate string `json:"voiceNameTemplate"`
	VoiceUserLimit    string `json:"voiceUserLimit"`
	VoicePrivacyMode  string `json:"voicePrivacy"`
}

type TicketSettings struct {
	TicketChannelID         string `json:"ticketChannelId"`
	TicketCategoryID        string `json:"ticketCategoryId"`
	TicketArchiveCategoryID string `json:"ticketArchiveCategoryId"`
	TicketStaffRoles        string `json:"ticketStaffRoleIds"`
	TicketOpenLimit         string `json:"ticketOpenLimit"`
	TicketTranscriptEnabled string `json:"ticketTranscriptEnabled"`
	TicketLogChannelID      string `json:"ticketLogChannelId"`
	TicketPanelTitle        string `json:"ticketPanelTitle"`
	TicketPanelBody         string `json:"ticketPanelBody"`
	TicketPanelColor        string `json:"ticketPanelColor"`
	TicketPanelButton       string `json:"ticketPanelButton"`
}

type LogsSettings struct {
	LogChannelID           string `json:"logChannelId"`
	LogMessagesEnabled     string `json:"logMessagesEnabled"`
	LogMembersEnabled      string `json:"logMembersEnabled"`
	LogVoiceEnabled        string `json:"logVoiceEnabled"`
	LogModerationEnabled   string `json:"logModerationEnabled"`
	LogChannelsEnabled     string `json:"logChannelsEnabled"`
	LogRolesEnabled        string `json:"logRolesEnabled"`
	LogServerEnabled       string `json:"logServerEnabled"`
	LogIgnoreBots          string `json:"logIgnoreBots"`
	LogMessagesChannelID   string `json:"logMessagesChannelId"`
	LogMembersChannelID    string `json:"logMembersChannelId"`
	LogVoiceChannelID      string `json:"logVoiceChannelId"`
	LogModerationChannelID string `json:"logModerationChannelId"`
	LogIgnoredChannels     string `json:"logIgnoredChannelIds"`
}

type LevelsSettings struct{}

type LinkGuardSettings struct {
	LinkAllowList string `json:"linkAllowList"`
}

type ModerationSettings struct{}

func (r RolesSettings) StaffRoleIDs() []string {
	out := make([]string, 0, 3)
	for _, id := range []string{r.OwnerRoleID, r.LeadModRoleID, r.ModsRoleID} {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (r RolesSettings) IsModStaff(memberRoles RoleIDs) bool {
	return holdsAny(memberRoles, r.StaffRoleIDs())
}

func (r RolesSettings) PinnedRoleMap() map[string]string {
	entries := splitList(listText(r.PinnedRoles))
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		pin, ok := splitPin(entry)
		if !ok {
			continue
		}
		out[string(pin.Slot)] = pin.ID
	}
	return out
}

func (r RolesSettings) PinnedRole(slot Slot) string { return r.PinnedRoleMap()[string(slot)] }

func (l LiveSettings) CategoryAllowed(category string) bool {
	cat := nameText(strings.TrimSpace(strings.ToLower(category)))
	if containsName(splitCSV(listText(l.CategoryDeny)), cat) {
		return false
	}
	allow := splitCSV(listText(l.CategoryAllow))
	return len(allow) == 0 || containsName(allow, cat)
}

func (l LiveSettings) HasCategoryAllow() bool { return len(splitCSV(listText(l.CategoryAllow))) > 0 }

func (s LinkGuardSettings) Allowed(raw string) bool {
	needle := strings.ToLower(strings.TrimSpace(raw))
	if needle == "" {
		return false
	}
	for _, entry := range splitCSV(listText(s.LinkAllowList)) {
		if strings.Contains(needle, entry) {
			return true
		}
	}
	return false
}

func (v VoiceSettings) Name(owner string) string {
	tpl := strings.TrimSpace(v.VoiceNameTemplate)
	if tpl == "" {
		return owner
	}
	name := strings.TrimSpace(strings.ReplaceAll(tpl, VoiceOwnerToken, owner))
	if name == "" {
		return owner
	}
	if r := []rune(name); len(r) > VoiceNameMax {
		return string(r[:VoiceNameMax])
	}
	return name
}

func (v VoiceSettings) CategorySet() bool { return strings.TrimSpace(v.VoiceCategoryID) != "" }

func (v VoiceSettings) Limit() int {
	n, err := strconv.Atoi(strings.TrimSpace(v.VoiceUserLimit))
	if err != nil || !withinVoiceLimit(n) {
		return 0
	}
	return n
}

func (v VoiceSettings) Privacy() string {
	switch mode := strings.TrimSpace(v.VoicePrivacyMode); mode {
	case VoicePrivacyLocked, VoicePrivacyHidden:
		return mode
	}
	return VoicePrivacyOpen
}

func (t TicketSettings) ArchiveCategory() string { return strings.TrimSpace(t.TicketArchiveCategoryID) }

func (t TicketSettings) TranscriptOn() bool { return alertOn(toggleText(t.TicketTranscriptEnabled)) }

func (t TicketSettings) StaffRoleIDs(roles RolesSettings) []string {
	if ids := splitList(listText(t.TicketStaffRoles)); len(ids) > 0 {
		return ids
	}
	return roles.StaffRoleIDs()
}

func (t TicketSettings) IsStaff(memberRoles RoleIDs, roles RolesSettings) bool {
	return roles.IsModStaff(memberRoles) || holdsAny(memberRoles, t.StaffRoleIDs(roles))
}

func (t TicketSettings) OpenLimitN() int {
	n, err := strconv.Atoi(strings.TrimSpace(t.TicketOpenLimit))
	if err != nil || n < 1 {
		return TicketOpenLimitDefault
	}
	if n > TicketOpenLimitMax {
		return TicketOpenLimitMax
	}
	return n
}

func (t TicketSettings) LogChannel(logs LogsSettings) string {
	if id := strings.TrimSpace(t.TicketLogChannelID); id != "" {
		return id
	}
	return strings.TrimSpace(logs.LogChannelID)
}

func (t TicketSettings) Panel() TicketPanelSpec {
	color := LiveColor
	if parsed, ok := ParseHexColor(t.TicketPanelColor); ok {
		color = parsed
	}
	return TicketPanelSpec{
		Title:  firstNonEmpty(copyText(t.TicketPanelTitle), TicketPanelTitleDefault),
		Body:   firstNonEmpty(copyText(t.TicketPanelBody), TicketPanelBodyDefault),
		Button: firstNonEmpty(copyText(t.TicketPanelButton), TicketPanelButtonDefault),
		Color:  &color,
	}
}

var logToggles = map[LogCategory]func(LogsSettings) string{
	LogMessages:   func(l LogsSettings) string { return l.LogMessagesEnabled },
	LogMembers:    func(l LogsSettings) string { return l.LogMembersEnabled },
	LogVoice:      func(l LogsSettings) string { return l.LogVoiceEnabled },
	LogModeration: func(l LogsSettings) string { return l.LogModerationEnabled },
	LogChannels:   func(l LogsSettings) string { return l.LogChannelsEnabled },
	LogRoles:      func(l LogsSettings) string { return l.LogRolesEnabled },
	LogServer:     func(l LogsSettings) string { return l.LogServerEnabled },
}

var logChannels = map[LogCategory]func(LogsSettings) string{
	LogMessages:   func(l LogsSettings) string { return l.LogMessagesChannelID },
	LogMembers:    func(l LogsSettings) string { return l.LogMembersChannelID },
	LogVoice:      func(l LogsSettings) string { return l.LogVoiceChannelID },
	LogModeration: func(l LogsSettings) string { return l.LogModerationChannelID },
}

func (l LogsSettings) CategoryOn(cat LogCategory) bool {
	toggle, ok := logToggles[cat]
	return ok && alertOn(toggleText(toggle(l)))
}

func (l LogsSettings) ChannelFor(cat LogCategory) string {
	if channel, ok := logChannels[cat]; ok {
		if id := strings.TrimSpace(channel(l)); id != "" {
			return id
		}
	}
	return strings.TrimSpace(l.LogChannelID)
}

func (l LogsSettings) IgnoredChannelIDs() []string { return splitList(listText(l.LogIgnoredChannels)) }

func (l LogsSettings) Ignores(channelID string) bool {
	id := strings.TrimSpace(channelID)
	return id != "" && slices.Contains(l.IgnoredChannelIDs(), id)
}

func (l LogsSettings) IgnoreBotsOn() bool { return alertOn(toggleText(l.LogIgnoreBots)) }
