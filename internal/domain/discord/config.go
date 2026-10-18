// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
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
func (c Config) GoodbyeOn() bool {
	return WelcomeSettings{GoodbyeEnabled: c.GoodbyeEnabled}.GoodbyeOn()
}
func (c Config) VoiceOn() bool   { return alertOn(toggleText(c.VoiceEnabled)) }
func (c Config) TicketsOn() bool { return alertOn(toggleText(c.TicketsEnabled)) }
func (c Config) LevelsOn() bool  { return alertOn(toggleText(c.LevelsEnabled)) }

func (c Config) AutoRoleOn() bool { return alertOn(toggleText(c.AutoRoleEnabled)) }
