// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"sort"
	"strings"
)

// FieldError names one rejected field by its JSON tag (the name the
// dashboard knows it by) and a stable machine code. Code, not a message:
// the console renders the copy in the streamer's language, and matching on
// English prose is the coupling this replaces (see the setup RPC's own
// error contract).
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Validation codes. Additive only; the console switches on them.
const (
	CodeInvalidID    = "invalid_id"
	CodeInvalidColor = "invalid_color"
	CodeInvalidRange = "out_of_range"
	CodeTooLong      = "too_long"
	CodeInvalidSlot  = "invalid_slot"
	CodeInvalidFlag  = "invalid_flag"
)

// Snowflake bounds. Discord ids are 64-bit millisecond-epoch snowflakes,
// which are 17 digits from 2016 and 19 today; 20 leaves room for the whole
// unsigned range. Anything outside is a paste error (a channel NAME, a
// mention with its <#…> wrapper, a truncated id) and is worth refusing at
// the form rather than as a 404 from Discord an hour later.
const (
	snowflakeMinLen = 17
	snowflakeMaxLen = 20
)

// ValidateConfig reports every field the dashboard must fix before the blob
// is worth writing. An empty slice means the config is storable; it does
// NOT mean every id still exists in the guild, which only Discord can say.
//
// Emptiness is never an error here. Every field on Config has a documented
// default (see config.go), and a half-filled config is the normal state of
// a guild mid-setup.
func ValidateConfig(cfg Config) []FieldError {
	var out []FieldError
	out = append(out, validateIDs(cfg)...)
	out = append(out, validateTicket(cfg)...)
	out = append(out, validatePinnedRoles(cfg.PinnedRoles)...)
	out = append(out, validateToggles(cfg)...)
	return out
}

// idFields is every snowflake-shaped field, by JSON tag.
func idFields(cfg Config) map[string]string {
	return map[string]string{
		"guildId":                 cfg.GuildID,
		"liveChannelId":           cfg.LiveChannelID,
		"clipsChannelId":          cfg.ClipsChannelID,
		"welcomeChannelId":        cfg.WelcomeChannelID,
		"voiceHubId":              cfg.VoiceHubID,
		"logChannelId":            cfg.LogChannelID,
		"ticketChannelId":         cfg.TicketChannelID,
		"ticketCategoryId":        cfg.TicketCategoryID,
		"ticketArchiveCategoryId": cfg.TicketArchiveCategoryID,
		"ticketLogChannelId":      cfg.TicketLogChannelID,
		"subsChannelId":           cfg.SubsChannelID,
		"subsCategoryId":          cfg.SubsCategoryID,
		"vipChannelId":            cfg.VIPChannelID,
		"vipCategoryId":           cfg.VIPCategoryID,
		"ownerRoleId":             cfg.OwnerRoleID,
		"leadModRoleId":           cfg.LeadModRoleID,
		"modsRoleId":              cfg.ModsRoleID,
		"vipRoleId":               cfg.VIPRoleID,
		"subscriberRoleId":        cfg.SubscriberRoleID,
		"regularsRoleId":          cfg.RegularsRoleID,
		"memberRoleId":            cfg.MemberRoleID,
	}
}

func validateIDs(cfg Config) []FieldError {
	var out []FieldError
	fields := idFields(cfg)
	for _, field := range sortedKeys(fields) {
		if !ValidSnowflake(fields[field]) {
			out = append(out, FieldError{Field: field, Code: CodeInvalidID})
		}
	}
	for _, id := range splitList(cfg.TicketStaffRoles) {
		if !ValidSnowflake(id) {
			out = append(out, FieldError{Field: "ticketStaffRoleIds", Code: CodeInvalidID})
			break
		}
	}
	return out
}

func validateTicket(cfg Config) []FieldError {
	var out []FieldError
	if cfg.TicketPanelColor != "" {
		if _, ok := ParseHexColor(cfg.TicketPanelColor); !ok {
			out = append(out, FieldError{Field: "ticketPanelColor", Code: CodeInvalidColor})
		}
	}
	out = append(out, tooLong("ticketPanelTitle", cfg.TicketPanelTitle, TicketPanelTitleMax)...)
	out = append(out, tooLong("ticketPanelBody", cfg.TicketPanelBody, TicketPanelBodyMax)...)
	out = append(out, tooLong("ticketPanelButton", cfg.TicketPanelButton, TicketPanelButtonMax)...)
	if !validLimit(cfg.TicketOpenLimit) {
		out = append(out, FieldError{Field: "ticketOpenLimit", Code: CodeInvalidRange})
	}
	return out
}

func validLimit(raw string) bool {
	v := strings.TrimSpace(raw)
	if v == "" {
		return true
	}
	return len(v) == 1 && v[0] >= '1' && v[0] <= byte('0'+TicketOpenLimitMax)
}

func tooLong(field, value string, max int) []FieldError {
	if len([]rune(value)) <= max {
		return nil
	}
	return []FieldError{{Field: field, Code: CodeTooLong}}
}

func validatePinnedRoles(raw string) []FieldError {
	var out []FieldError
	for _, entry := range splitList(raw) {
		_, id, ok := splitPin(entry)
		if !ok {
			out = append(out, FieldError{Field: "pinnedRoles", Code: CodeInvalidSlot})
			continue
		}
		if !ValidSnowflake(id) {
			out = append(out, FieldError{Field: "pinnedRoles", Code: CodeInvalidID})
		}
	}
	return out
}

// toggleFields is every "on"/"off" string field, by JSON tag.
func toggleFields(cfg Config) map[string]string {
	return map[string]string{
		"liveEnabled":             cfg.LiveEnabled,
		"clipsEnabled":            cfg.ClipsEnabled,
		"welcomeEnabled":          cfg.WelcomeEnabled,
		"goodbyeEnabled":          cfg.GoodbyeEnabled,
		"voiceEnabled":            cfg.VoiceEnabled,
		"ticketsEnabled":          cfg.TicketsEnabled,
		"logsEnabled":             cfg.LogsEnabled,
		"levelsEnabled":           cfg.LevelsEnabled,
		"linkGuardEnabled":        cfg.LinkGuardEnabled,
		"subscribersEnabled":      cfg.SubscribersEnabled,
		"ticketTranscriptEnabled": cfg.TicketTranscriptEnabled,
		"autoRoleEnabled":         cfg.AutoRoleEnabled,
	}
}

func validateToggles(cfg Config) []FieldError {
	var out []FieldError
	fields := toggleFields(cfg)
	for _, field := range sortedKeys(fields) {
		switch fields[field] {
		case "", "on", "off":
		default:
			out = append(out, FieldError{Field: field, Code: CodeInvalidFlag})
		}
	}
	return out
}

// ValidSnowflake reports whether id is empty (unset, always allowed) or
// looks like a Discord snowflake: digits only, snowflakeMinLen..MaxLen.
func ValidSnowflake(id string) bool {
	if id == "" {
		return true
	}
	if len(id) < snowflakeMinLen || len(id) > snowflakeMaxLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

// sortedKeys keeps ValidateConfig's output stable across runs. Go's map
// iteration is deliberately randomized, and an error list that reorders
// itself between two identical saves makes the dashboard's field
// highlighting flicker and makes this function untestable by equality.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
