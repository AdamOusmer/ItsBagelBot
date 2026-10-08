// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc/discorddata"
)

type configRPC struct{ repo ConfigStore }

func subscribeConfigs(w Wiring) error {
	h := configRPC{repo: w.Repo}
	return errors.Join(
		serve(w, discorddata.VerbConfigGet, h.get),
		serve(w, discorddata.VerbConfigSet, h.set),
	)
}

func (h configRPC) get(ctx context.Context, req discorddata.ConfigGetRequest) discorddata.ConfigGetReply {
	cfg, version, found, err := h.repo.ConfigGet(ctx, req.GuildID)
	if err != nil {
		message, code := failure(err)
		return discorddata.ConfigGetReply{Error: message, Code: code}
	}
	return discorddata.ConfigGetReply{Config: cfg, Version: version, Found: found}
}

func (h configRPC) set(ctx context.Context, req discorddata.ConfigSetRequest) discorddata.ConfigSetReply {
	if bad := validateConfig(req.Config); len(bad) > 0 {
		return discorddata.ConfigSetReply{
			Fields: bad,
			Error:  repository.ErrInvalidInput.Error(),
			Code:   discorddata.CodeInvalid,
		}
	}
	version, err := h.repo.ConfigSet(ctx, repository.SetConfigParams{
		GuildID:         req.GuildID,
		BroadcasterID:   req.BroadcasterID,
		Config:          req.Config,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		message, code := failure(err)
		return discorddata.ConfigSetReply{Error: message, Code: code}
	}
	return discorddata.ConfigSetReply{Version: version}
}

const (
	snowflakeMinDigits = 17
	snowflakeMaxDigits = 20
)

type field struct {
	name    string
	value   string
	min     int
	max     int
	choices []string
}

func validateConfig(c ddiscord.Config) []string {
	bad := invalidNames(snowflakeFields(c), validSnowflake)
	bad = append(bad, invalidNames(snowflakeListFields(c), validSnowflakeList)...)
	bad = append(bad, invalidNames(toggleFields(c), validToggle)...)
	bad = append(bad, invalidNames(rangeFields(c), validRange)...)
	return append(bad, invalidNames(voiceTextFields(c), validVoiceText)...)
}

func invalidNames(fields []field, ok func(field) bool) []string {
	var bad []string
	for _, f := range fields {
		if !ok(f) {
			bad = append(bad, f.name)
		}
	}
	return bad
}

func validSnowflake(f field) bool { return validSnowflakeValue(f.value) }

func validSnowflakeValue(v string) bool {
	if v == "" {
		return true
	}
	if len(v) < snowflakeMinDigits || len(v) > snowflakeMaxDigits {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validSnowflakeList(f field) bool {
	count := 0
	for _, part := range strings.Split(f.value, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if !validSnowflakeValue(p) {
			return false
		}
		count++
	}
	return f.max == 0 || count <= f.max
}

func validToggle(f field) bool { return f.value == "" || f.value == "on" || f.value == "off" }

func validRange(f field) bool {
	v := strings.TrimSpace(f.value)
	if v == "" {
		return true
	}
	if len(v) > 2 || v[0] < '0' || v[0] > '9' {
		return false
	}
	n, err := strconv.Atoi(v)
	return err == nil && n >= f.min && n <= f.max
}

func validVoiceText(f field) bool {
	if f.choices != nil {
		return f.value == "" || slices.Contains(f.choices, f.value)
	}
	return len([]rune(f.value)) <= f.max
}

func snowflakeFields(c ddiscord.Config) []field {
	return []field{
		{name: "guildId", value: c.GuildID},
		{name: "liveChannelId", value: c.LiveChannelID},
		{name: "clipsChannelId", value: c.ClipsChannelID},
		{name: "welcomeChannelId", value: c.WelcomeChannelID},
		{name: "voiceHubId", value: c.VoiceHubID},
		{name: "voiceCategoryId", value: c.VoiceCategoryID},
		{name: "logChannelId", value: c.LogChannelID},
		{name: "logMessagesChannelId", value: c.LogMessagesChannelID},
		{name: "logMembersChannelId", value: c.LogMembersChannelID},
		{name: "logVoiceChannelId", value: c.LogVoiceChannelID},
		{name: "logModerationChannelId", value: c.LogModerationChannelID},
		{name: "ticketChannelId", value: c.TicketChannelID},
		{name: "ticketCategoryId", value: c.TicketCategoryID},
		{name: "ticketArchiveCategoryId", value: c.TicketArchiveCategoryID},
		{name: "ticketLogChannelId", value: c.TicketLogChannelID},
		{name: "subsChannelId", value: c.SubsChannelID},
		{name: "subsCategoryId", value: c.SubsCategoryID},
		{name: "vipChannelId", value: c.VIPChannelID},
		{name: "vipCategoryId", value: c.VIPCategoryID},
		{name: "ownerRoleId", value: c.OwnerRoleID},
		{name: "leadModRoleId", value: c.LeadModRoleID},
		{name: "modsRoleId", value: c.ModsRoleID},
		{name: "vipRoleId", value: c.VIPRoleID},
		{name: "subscriberRoleId", value: c.SubscriberRoleID},
		{name: "regularsRoleId", value: c.RegularsRoleID},
		{name: "memberRoleId", value: c.MemberRoleID},
	}
}

func snowflakeListFields(c ddiscord.Config) []field {
	return []field{
		{name: "ticketStaffRoleIds", value: c.TicketStaffRoles},
		{name: "logIgnoredChannelIds", value: c.LogIgnoredChannels, max: ddiscord.LogIgnoredChannelsMax},
	}
}

func toggleFields(c ddiscord.Config) []field {
	return []field{
		{name: "liveEnabled", value: c.LiveEnabled},
		{name: "clipsEnabled", value: c.ClipsEnabled},
		{name: "welcomeEnabled", value: c.WelcomeEnabled},
		{name: "goodbyeEnabled", value: c.GoodbyeEnabled},
		{name: "voiceEnabled", value: c.VoiceEnabled},
		{name: "ticketsEnabled", value: c.TicketsEnabled},
		{name: "logsEnabled", value: c.LogsEnabled},
		{name: "levelsEnabled", value: c.LevelsEnabled},
		{name: "linkGuardEnabled", value: c.LinkGuardEnabled},
		{name: "subscribersEnabled", value: c.SubscribersEnabled},
		{name: "ticketTranscriptEnabled", value: c.TicketTranscriptEnabled},
		{name: "autoRoleEnabled", value: c.AutoRoleEnabled},
		{name: "logMessagesEnabled", value: c.LogMessagesEnabled},
		{name: "logMembersEnabled", value: c.LogMembersEnabled},
		{name: "logVoiceEnabled", value: c.LogVoiceEnabled},
		{name: "logModerationEnabled", value: c.LogModerationEnabled},
		{name: "logChannelsEnabled", value: c.LogChannelsEnabled},
		{name: "logRolesEnabled", value: c.LogRolesEnabled},
		{name: "logServerEnabled", value: c.LogServerEnabled},
		{name: "logIgnoreBots", value: c.LogIgnoreBots},
	}
}

func rangeFields(c ddiscord.Config) []field {
	return []field{
		{name: "ticketOpenLimit", value: c.TicketOpenLimit, min: 1, max: ddiscord.TicketOpenLimitMax},
		{name: "voiceUserLimit", value: c.VoiceUserLimit, min: 0, max: ddiscord.VoiceUserLimitMax},
	}
}

func voiceTextFields(c ddiscord.Config) []field {
	return []field{
		{name: "voiceNameTemplate", value: c.VoiceNameTemplate, max: ddiscord.VoiceNameMax},
		{name: "voicePrivacy", value: c.VoicePrivacyMode, choices: []string{
			ddiscord.VoicePrivacyOpen, ddiscord.VoicePrivacyLocked, ddiscord.VoicePrivacyHidden,
		}},
	}
}
