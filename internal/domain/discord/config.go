// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/pkg/codec"
)

const ModuleName = "discord"

type Config struct {
	GuildID          string `json:"guildId"`
	LiveChannelID    string `json:"liveChannelId"`
	ClipsChannelID   string `json:"clipsChannelId"`
	WelcomeChannelID string `json:"welcomeChannelId"`
	VoiceHubID       string `json:"voiceHubId"`
	LogChannelID     string `json:"logChannelId"`
	TicketChannelID  string `json:"ticketChannelId"`
	TicketCategoryID string `json:"ticketCategoryId"`
	OwnerRoleID      string `json:"ownerRoleId"`
	LeadModRoleID    string `json:"leadModRoleId"`
	ModsRoleID       string `json:"modsRoleId"`
	VIPRoleID        string `json:"vipRoleId"`
	SubscriberRoleID string `json:"subscriberRoleId"`
	RegularsRoleID   string `json:"regularsRoleId"`
	MemberRoleID     string `json:"memberRoleId"`

	SubsChannelID  string `json:"subsChannelId"`
	SubsCategoryID string `json:"subsCategoryId"`
	VIPChannelID   string `json:"vipChannelId"`
	VIPCategoryID  string `json:"vipCategoryId"`

	TicketArchiveCategoryID string `json:"ticketArchiveCategoryId"`
	TicketStaffRoles        string `json:"ticketStaffRoleIds"`
	TicketOpenLimit         string `json:"ticketOpenLimit"`
	TicketTranscriptEnabled string `json:"ticketTranscriptEnabled"`
	TicketLogChannelID      string `json:"ticketLogChannelId"`
	TicketPanelTitle        string `json:"ticketPanelTitle"`
	TicketPanelBody         string `json:"ticketPanelBody"`
	TicketPanelColor        string `json:"ticketPanelColor"`
	TicketPanelButton       string `json:"ticketPanelButton"`

	PinnedRoles     string `json:"pinnedRoles"`
	AutoRoleEnabled string `json:"autoRoleEnabled"`

	LiveEnabled        string `json:"liveEnabled"`
	ClipsEnabled       string `json:"clipsEnabled"`
	WelcomeEnabled     string `json:"welcomeEnabled"`
	GoodbyeEnabled     string `json:"goodbyeEnabled"`
	VoiceEnabled       string `json:"voiceEnabled"`
	TicketsEnabled     string `json:"ticketsEnabled"`
	LogsEnabled        string `json:"logsEnabled"`
	LevelsEnabled      string `json:"levelsEnabled"`
	LinkGuardEnabled   string `json:"linkGuardEnabled"`
	SubscribersEnabled string `json:"subscribersEnabled"`

	LogMessagesEnabled   string `json:"logMessagesEnabled"`
	LogMembersEnabled    string `json:"logMembersEnabled"`
	LogVoiceEnabled      string `json:"logVoiceEnabled"`
	LogModerationEnabled string `json:"logModerationEnabled"`
	LogChannelsEnabled   string `json:"logChannelsEnabled"`
	LogRolesEnabled      string `json:"logRolesEnabled"`
	LogServerEnabled     string `json:"logServerEnabled"`
	LogIgnoreBots        string `json:"logIgnoreBots"`

	LogMessagesChannelID   string `json:"logMessagesChannelId"`
	LogMembersChannelID    string `json:"logMembersChannelId"`
	LogVoiceChannelID      string `json:"logVoiceChannelId"`
	LogModerationChannelID string `json:"logModerationChannelId"`
	LogIgnoredChannels     string `json:"logIgnoredChannelIds"`

	VoiceCategoryID   string `json:"voiceCategoryId"`
	VoiceNameTemplate string `json:"voiceNameTemplate"`
	VoiceUserLimit    string `json:"voiceUserLimit"`
	VoicePrivacyMode  string `json:"voicePrivacy"`

	CategoryAllow string `json:"categoryAllow"`
	CategoryDeny  string `json:"categoryDeny"`

	LinkAllowList string `json:"linkAllowList"`

	TwitchLogin string `json:"twitchLogin"`
}

func Parse(raw []byte) Config {
	var c Config
	if len(raw) == 0 {
		return c
	}
	_ = codec.Unmarshal(raw, &c)
	return c
}

func (c Config) Connected() bool { return strings.TrimSpace(c.GuildID) != "" }

type (
	toggleText string
	listText   string
	nameText   string
	copyText   string
)

func alertOn(v toggleText) bool { return v != "off" }

func (c Config) LiveOn() bool    { return alertOn(toggleText(c.LiveEnabled)) }
func (c Config) ClipsOn() bool   { return alertOn(toggleText(c.ClipsEnabled)) }
func (c Config) WelcomeOn() bool { return alertOn(toggleText(c.WelcomeEnabled)) }
func (c Config) GoodbyeOn() bool { return c.GoodbyeEnabled == "on" }
func (c Config) VoiceOn() bool   { return alertOn(toggleText(c.VoiceEnabled)) }
func (c Config) TicketsOn() bool { return alertOn(toggleText(c.TicketsEnabled)) }
func (c Config) LogsOn() bool    { return alertOn(toggleText(c.LogsEnabled)) }
func (c Config) LevelsOn() bool  { return alertOn(toggleText(c.LevelsEnabled)) }

func (c Config) TicketTranscriptOn() bool { return alertOn(toggleText(c.TicketTranscriptEnabled)) }

func (c Config) AutoRoleOn() bool { return alertOn(toggleText(c.AutoRoleEnabled)) }

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

var logToggles = map[LogCategory]func(Config) string{
	LogMessages:   func(c Config) string { return c.LogMessagesEnabled },
	LogMembers:    func(c Config) string { return c.LogMembersEnabled },
	LogVoice:      func(c Config) string { return c.LogVoiceEnabled },
	LogModeration: func(c Config) string { return c.LogModerationEnabled },
	LogChannels:   func(c Config) string { return c.LogChannelsEnabled },
	LogRoles:      func(c Config) string { return c.LogRolesEnabled },
	LogServer:     func(c Config) string { return c.LogServerEnabled },
}

var logChannels = map[LogCategory]func(Config) string{
	LogMessages:   func(c Config) string { return c.LogMessagesChannelID },
	LogMembers:    func(c Config) string { return c.LogMembersChannelID },
	LogVoice:      func(c Config) string { return c.LogVoiceChannelID },
	LogModeration: func(c Config) string { return c.LogModerationChannelID },
}

func (c Config) LogCategoryOn(cat LogCategory) bool {
	toggle, ok := logToggles[cat]
	return ok && c.LogsOn() && alertOn(toggleText(toggle(c)))
}

func (c Config) LogChannelFor(cat LogCategory) string {
	if channel, ok := logChannels[cat]; ok {
		if id := strings.TrimSpace(channel(c)); id != "" {
			return id
		}
	}
	return strings.TrimSpace(c.LogChannelID)
}

func (c Config) LogIgnoredChannelIDs() []string { return splitList(listText(c.LogIgnoredChannels)) }

func (c Config) LogIgnores(channelID string) bool {
	id := strings.TrimSpace(channelID)
	return id != "" && slices.Contains(c.LogIgnoredChannelIDs(), id)
}

const LogIgnoredChannelsMax = 25

func (c Config) LogIgnoreBotsOn() bool { return alertOn(toggleText(c.LogIgnoreBots)) }

const (
	VoiceOwnerToken    = "{owner}"
	VoiceNameMax       = 100
	VoiceUserLimitMax  = 99
	VoicePrivacyOpen   = "open"
	VoicePrivacyLocked = "locked"
	VoicePrivacyHidden = "hidden"
)

func (c Config) VoiceName(owner string) string {
	tpl := strings.TrimSpace(c.VoiceNameTemplate)
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

func (c Config) VoiceCategorySet() bool { return strings.TrimSpace(c.VoiceCategoryID) != "" }

func (c Config) VoiceLimit() int {
	n, err := strconv.Atoi(strings.TrimSpace(c.VoiceUserLimit))
	if err != nil || !withinVoiceLimit(n) {
		return 0
	}
	return n
}

func withinVoiceLimit(n int) bool { return n >= 0 && n <= VoiceUserLimitMax }

func (c Config) VoicePrivacy() string {
	switch mode := strings.TrimSpace(c.VoicePrivacyMode); mode {
	case VoicePrivacyLocked, VoicePrivacyHidden:
		return mode
	}
	return VoicePrivacyOpen
}

type TierRooms struct {
	SubsChannelID  string
	SubsCategoryID string
	VIPChannelID   string
	VIPCategoryID  string
}

func (c Config) TierRooms() TierRooms {
	return TierRooms{
		SubsChannelID:  c.SubsChannelID,
		SubsCategoryID: c.SubsCategoryID,
		VIPChannelID:   c.VIPChannelID,
		VIPCategoryID:  c.VIPCategoryID,
	}
}

func (c Config) TicketArchiveCategory() string { return strings.TrimSpace(c.TicketArchiveCategoryID) }

func (c Config) TicketStaffRoleIDs() []string {
	if ids := splitList(listText(c.TicketStaffRoles)); len(ids) > 0 {
		return ids
	}
	return c.StaffRoleIDs()
}

const TicketOpenLimitDefault = 1

const TicketOpenLimitMax = 5

func (c Config) TicketOpenLimitN() int {
	n, err := strconv.Atoi(strings.TrimSpace(c.TicketOpenLimit))
	if err != nil || n < 1 {
		return TicketOpenLimitDefault
	}
	if n > TicketOpenLimitMax {
		return TicketOpenLimitMax
	}
	return n
}

func (c Config) TicketLogChannel() string {
	if id := strings.TrimSpace(c.TicketLogChannelID); id != "" {
		return id
	}
	return strings.TrimSpace(c.LogChannelID)
}

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

func (c Config) TicketPanel() TicketPanelSpec {
	color := LiveColor
	if parsed, ok := ParseHexColor(c.TicketPanelColor); ok {
		color = parsed
	}
	return TicketPanelSpec{
		Title:  firstNonEmpty(copyText(c.TicketPanelTitle), TicketPanelTitleDefault),
		Body:   firstNonEmpty(copyText(c.TicketPanelBody), TicketPanelBodyDefault),
		Button: firstNonEmpty(copyText(c.TicketPanelButton), TicketPanelButtonDefault),
		Color:  &color,
	}
}

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

func (c Config) LinkGuardOn() bool { return c.LinkGuardEnabled == "on" }

func (c Config) SubscribersOn() bool { return c.SubscribersEnabled == "on" }

func (c Config) CategoryAllowed(category string) bool {
	cat := nameText(strings.TrimSpace(strings.ToLower(category)))
	if containsName(splitCSV(listText(c.CategoryDeny)), cat) {
		return false
	}
	allow := splitCSV(listText(c.CategoryAllow))
	return len(allow) == 0 || containsName(allow, cat)
}

func (c Config) HasCategoryAllow() bool { return len(splitCSV(listText(c.CategoryAllow))) > 0 }

func (c Config) LinkAllowed(raw string) bool {
	needle := strings.ToLower(strings.TrimSpace(raw))
	if needle == "" {
		return false
	}
	for _, entry := range splitCSV(listText(c.LinkAllowList)) {
		if strings.Contains(needle, entry) {
			return true
		}
	}
	return false
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

func (c Config) StaffRoleIDs() []string {
	out := make([]string, 0, 3)
	for _, id := range []string{c.OwnerRoleID, c.LeadModRoleID, c.ModsRoleID} {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}
