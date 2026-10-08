// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

const (
	CodeInvalidID     = "invalid_id"
	CodeInvalidColor  = "invalid_color"
	CodeInvalidRange  = "out_of_range"
	CodeTooLong       = "too_long"
	CodeInvalidSlot   = "invalid_slot"
	CodeInvalidFlag   = "invalid_flag"
	CodeDuplicateSlot = "duplicate_slot"
	CodeMalformedPair = "malformed_pair"
	CodeInvalidChoice = "invalid_choice"
)

const pinnedRolesField = "pinnedRoles"

const (
	snowflakeMinLen = 17
	snowflakeMaxLen = 20
)

func ValidateConfig(cfg Config) []FieldError {
	var out []FieldError
	out = append(out, validateIDs(cfg)...)
	out = append(out, validateTicket(cfg)...)
	out = append(out, validatePinnedRoles(listText(cfg.PinnedRoles))...)
	out = append(out, validateToggles(cfg)...)
	out = append(out, validateVoice(cfg)...)
	return out
}

func idFields(cfg Config) map[string]string {
	return map[string]string{
		"guildId":                 cfg.GuildID,
		"liveChannelId":           cfg.LiveChannelID,
		"clipsChannelId":          cfg.ClipsChannelID,
		"welcomeChannelId":        cfg.WelcomeChannelID,
		"voiceHubId":              cfg.VoiceHubID,
		"voiceCategoryId":         cfg.VoiceCategoryID,
		"logChannelId":            cfg.LogChannelID,
		"logMessagesChannelId":    cfg.LogMessagesChannelID,
		"logMembersChannelId":     cfg.LogMembersChannelID,
		"logVoiceChannelId":       cfg.LogVoiceChannelID,
		"logModerationChannelId":  cfg.LogModerationChannelID,
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
	out := invalidIDFields(idFields(cfg))
	out = append(out, invalidIDList("ticketStaffRoleIds", listText(cfg.TicketStaffRoles))...)
	return append(out, invalidIDList("logIgnoredChannelIds", listText(cfg.LogIgnoredChannels))...)
}

func invalidIDFields(fields map[string]string) []FieldError {
	var out []FieldError
	for _, field := range sortedKeys(fields) {
		if !ValidSnowflake(fields[field]) {
			out = append(out, FieldError{Field: field, Code: CodeInvalidID})
		}
	}
	return out
}

func invalidIDList(field string, raw listText) []FieldError {
	for _, id := range splitList(raw) {
		if !ValidSnowflake(id) {
			return []FieldError{{Field: field, Code: CodeInvalidID}}
		}
	}
	return nil
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
	if len(v) != 1 {
		return false
	}
	return v[0] >= '1' && v[0] <= byte('0'+TicketOpenLimitMax)
}

func validateVoice(cfg Config) []FieldError {
	out := tooLong("voiceNameTemplate", cfg.VoiceNameTemplate, VoiceNameMax)
	if !validVoiceLimit(cfg.VoiceUserLimit) {
		out = append(out, FieldError{Field: "voiceUserLimit", Code: CodeInvalidRange})
	}
	switch cfg.VoicePrivacyMode {
	case "", VoicePrivacyOpen, VoicePrivacyLocked, VoicePrivacyHidden:
	default:
		out = append(out, FieldError{Field: "voicePrivacy", Code: CodeInvalidChoice})
	}
	return out
}

func validVoiceLimit(raw string) bool {
	v := strings.TrimSpace(raw)
	if v == "" {
		return true
	}
	if len(v) > 2 || v[0] < '0' || v[0] > '9' {
		return false
	}
	n, err := strconv.Atoi(v)
	return err == nil && n <= VoiceUserLimitMax
}

func tooLong(field, value string, max int) []FieldError {
	if len([]rune(value)) <= max {
		return nil
	}
	return []FieldError{{Field: field, Code: CodeTooLong}}
}

func validatePinnedRoles(raw listText) []FieldError {
	var out []FieldError
	seen := make(map[Slot]bool)
	for _, entry := range splitList(raw) {
		out = append(out, validatePin(entry, seen)...)
	}
	return out
}

func validatePin(entry string, seen map[Slot]bool) []FieldError {
	pin, ok := cutPin(entry)
	if !ok {
		return []FieldError{{Field: pinnedRolesField, Code: CodeMalformedPair}}
	}
	if !ValidSlot(pin.Slot) {
		return []FieldError{{Field: pinnedRolesField, Code: CodeInvalidSlot}}
	}
	if seen[pin.Slot] {
		return []FieldError{{Field: pinnedRolesField, Code: CodeDuplicateSlot}}
	}
	seen[pin.Slot] = true
	if !ValidSnowflake(pin.ID) {
		return []FieldError{{Field: pinnedRolesField, Code: CodeInvalidID}}
	}
	return nil
}

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
		"logMessagesEnabled":      cfg.LogMessagesEnabled,
		"logMembersEnabled":       cfg.LogMembersEnabled,
		"logVoiceEnabled":         cfg.LogVoiceEnabled,
		"logModerationEnabled":    cfg.LogModerationEnabled,
		"logChannelsEnabled":      cfg.LogChannelsEnabled,
		"logRolesEnabled":         cfg.LogRolesEnabled,
		"logServerEnabled":        cfg.LogServerEnabled,
		"logIgnoreBots":           cfg.LogIgnoreBots,
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

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func SanitizeConfig(cfg Config) (Config, []FieldError) {
	bad := ValidateConfig(cfg)
	if len(bad) == 0 {
		return cfg, nil
	}
	v := reflect.ValueOf(&cfg).Elem()
	byTag := configFieldsByTag(v.Type())
	for _, fe := range bad {
		if i, ok := byTag[fe.Field]; ok {
			v.Field(i).SetString("")
		}
	}
	return cfg, bad
}

func configFieldsByTag(t reflect.Type) map[string]int {
	out := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type.Kind() != reflect.String {
			continue
		}
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag != "" && tag != "-" {
			out[tag] = i
		}
	}
	return out
}
