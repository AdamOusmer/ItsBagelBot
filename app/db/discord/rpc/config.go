// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/pkg/bus"
)

type configRPC struct{ repo ConfigStore }

// subscribeConfigs registers the per-guild settings verbs.
func subscribeConfigs(w Wiring) error {
	h := configRPC{repo: w.Repo}
	if err := bus.QueueSubscribeJSON[discorddata.ConfigGetRequest, discorddata.ConfigGetReply](
		w.NC, w.subject(discorddata.VerbConfigGet), w.QueueGroup, requestTimeout, w.App, w.Log, h.get); err != nil {
		return err
	}
	return bus.QueueSubscribeJSON[discorddata.ConfigSetRequest, discorddata.ConfigSetReply](
		w.NC, w.subject(discorddata.VerbConfigSet), w.QueueGroup, requestTimeout, w.App, w.Log, h.set)
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

// snowflakeMinDigits / snowflakeMaxDigits bound a Discord id. Snowflakes are
// 64-bit, so 20 digits is the ceiling; the floor is 17 because Discord's epoch
// starts in 2015 and every id minted since is at least that long. Rejecting
// shorter ids catches the one mistake the dashboard can actually make -- a
// channel NAME pasted into an id field -- without this service having to ask
// Discord whether the id exists.
const (
	snowflakeMinDigits = 17
	snowflakeMaxDigits = 20
)

// field is one settings value paired with the json name the dashboard knows it
// by, so a refusal names the control the streamer has to fix.
type field struct {
	name  string
	value string
}

// validateConfig reports the settings that are malformed, by json name.
//
// This is a local check, not the domain's: ddiscord.ValidateConfig lands with
// the roles work (it validates the pinned-role slots and the ticket panel
// spec, neither of which exists on this branch's Config). When it arrives this
// function becomes a call to it -- the shape here is deliberately the same
// []string of json names.
func validateConfig(c ddiscord.Config) []string {
	bad := invalidNames(snowflakeFields(c), validSnowflake)
	return append(bad, invalidNames(toggleFields(c), validToggle)...)
}

// invalidNames returns the json names of the fields ok rejects. The two kinds
// of setting differ only in the predicate, so the walk is written once; the
// caller keeps the order (ids, then toggles) the dashboard renders the
// refusal in.
func invalidNames(fields []field, ok func(string) bool) []string {
	var bad []string
	for _, f := range fields {
		if !ok(f.value) {
			bad = append(bad, f.name)
		}
	}
	return bad
}

// validSnowflake accepts an empty value (the control is simply unset) or a
// plausible Discord id.
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

// validToggle accepts the dashboard's three states: unset (the documented
// default applies), "on", "off".
func validToggle(v string) bool { return v == "" || v == "on" || v == "off" }

// snowflakeFields lists every id-shaped setting with its json name.
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

// toggleFields lists every on/off setting with its json name.
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
