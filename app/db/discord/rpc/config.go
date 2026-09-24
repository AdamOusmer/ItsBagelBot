// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

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
	name  string
	value string
}

func validateConfig(c ddiscord.Config) []string {
	bad := invalidNames(snowflakeFields(c), validSnowflake)
	return append(bad, invalidNames(toggleFields(c), validToggle)...)
}

func invalidNames(fields []field, ok func(string) bool) []string {
	var bad []string
	for _, f := range fields {
		if !ok(f.value) {
			bad = append(bad, f.name)
		}
	}
	return bad
}

func validSnowflake(v string) bool {
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

func validToggle(v string) bool { return v == "" || v == "on" || v == "off" }

func snowflakeFields(c ddiscord.Config) []field {
	return []field{
		{"guildId", c.GuildID},
		{"liveChannelId", c.LiveChannelID},
		{"clipsChannelId", c.ClipsChannelID},
		{"welcomeChannelId", c.WelcomeChannelID},
		{"voiceHubId", c.VoiceHubID},
		{"logChannelId", c.LogChannelID},
		{"ticketChannelId", c.TicketChannelID},
		{"ticketCategoryId", c.TicketCategoryID},
		{"ownerRoleId", c.OwnerRoleID},
		{"leadModRoleId", c.LeadModRoleID},
		{"modsRoleId", c.ModsRoleID},
		{"vipRoleId", c.VIPRoleID},
		{"subscriberRoleId", c.SubscriberRoleID},
		{"regularsRoleId", c.RegularsRoleID},
		{"memberRoleId", c.MemberRoleID},
	}
}

func toggleFields(c ddiscord.Config) []field {
	return []field{
		{"liveEnabled", c.LiveEnabled},
		{"clipsEnabled", c.ClipsEnabled},
		{"welcomeEnabled", c.WelcomeEnabled},
		{"goodbyeEnabled", c.GoodbyeEnabled},
		{"voiceEnabled", c.VoiceEnabled},
		{"ticketsEnabled", c.TicketsEnabled},
		{"logsEnabled", c.LogsEnabled},
		{"levelsEnabled", c.LevelsEnabled},
		{"linkGuardEnabled", c.LinkGuardEnabled},
		{"subscribersEnabled", c.SubscribersEnabled},
	}
}
