// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discord holds the module blob and community-template contracts
// shared by outgress (live posts, setup fill), dingress (welcomes,
// auto-voice, tickets, slash), and the dashboard.
package discord

import (
	"strings"

	"ItsBagelBot/pkg/codec"
)

// ModuleName is the ModuleView key. The dashboard Discord tile writes this
// row; outgress reads it on stream.online without going through sesame.
const ModuleName = "discord"

// Config is the module blob. Channel and role snowflakes are not secrets;
// the fleet bot token never rides here.
type Config struct {
	GuildID          string `json:"guildId"`
	LiveChannelID    string `json:"liveChannelId"`
	ClipsChannelID   string `json:"clipsChannelId"`
	WelcomeChannelID string `json:"welcomeChannelId"`
	VoiceHubID       string `json:"voiceHubId"`
	LogChannelID     string `json:"logChannelId"`
	TicketChannelID  string `json:"ticketChannelId"`
	TicketCategoryID string `json:"ticketCategoryId"`
	LiveRoleID       string `json:"liveRoleId"`
	ModsRoleID       string `json:"modsRoleId"`
	RegularsRoleID   string `json:"regularsRoleId"`
	MemberRoleID     string `json:"memberRoleId"`

	// Toggles are dashboard "on"/"off" strings. Empty means the default
	// documented on each helper (live/clips on; goodbye off).
	LiveEnabled    string `json:"liveEnabled"`
	ClipsEnabled   string `json:"clipsEnabled"`
	WelcomeEnabled string `json:"welcomeEnabled"`
	GoodbyeEnabled string `json:"goodbyeEnabled"`
	VoiceEnabled   string `json:"voiceEnabled"`
	TicketsEnabled string `json:"ticketsEnabled"`
	LogsEnabled    string `json:"logsEnabled"`
	LevelsEnabled  string `json:"levelsEnabled"`

	// CategoryAllow / CategoryDeny are comma-separated Twitch category
	// names (Scenes twin). Empty allow means every category; a deny match
	// always wins.
	CategoryAllow string `json:"categoryAllow"`
	CategoryDeny  string `json:"categoryDeny"`

	TwitchLogin       string `json:"twitchLogin"`
	StreamerDiscordID string `json:"streamerDiscordId"`
}

// Parse decodes a module blob. An empty or malformed blob is a zero Config
// (not connected), so callers can treat that as "do nothing".
func Parse(raw []byte) Config {
	var c Config
	if len(raw) == 0 {
		return c
	}
	_ = codec.Unmarshal(raw, &c)
	return c
}

// Connected is a guild id we can actually post into.
func (c Config) Connected() bool { return strings.TrimSpace(c.GuildID) != "" }

func alertOn(v string) bool { return v != "off" }

func (c Config) LiveOn() bool    { return alertOn(c.LiveEnabled) }
func (c Config) ClipsOn() bool   { return alertOn(c.ClipsEnabled) }
func (c Config) WelcomeOn() bool { return alertOn(c.WelcomeEnabled) }
func (c Config) GoodbyeOn() bool { return c.GoodbyeEnabled == "on" }
func (c Config) VoiceOn() bool   { return alertOn(c.VoiceEnabled) }
func (c Config) TicketsOn() bool { return alertOn(c.TicketsEnabled) }
func (c Config) LogsOn() bool    { return alertOn(c.LogsEnabled) }
func (c Config) LevelsOn() bool  { return alertOn(c.LevelsEnabled) }

// CategoryAllowed reports whether a Twitch category should produce a go-live
// embed. Names compare case-insensitively, trimmed.
func (c Config) CategoryAllowed(category string) bool {
	cat := strings.TrimSpace(strings.ToLower(category))
	if containsName(splitCSV(c.CategoryDeny), cat) {
		return false
	}
	allow := splitCSV(c.CategoryAllow)
	return len(allow) == 0 || containsName(allow, cat)
}

// HasCategoryAllow reports whether an allow-list is set, which is when an
// unknown category cannot be decided and the caller must fetch it.
func (c Config) HasCategoryAllow() bool { return len(splitCSV(c.CategoryAllow)) > 0 }

// containsName is a membership test on splitCSV output; an empty needle
// never matches because splitCSV drops empty entries.
func containsName(list []string, needle string) bool {
	if needle == "" {
		return false
	}
	for _, v := range list {
		if v == needle {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.ToLower(strings.TrimSpace(p)); t != "" {
			out = append(out, t)
		}
	}
	return out
}
