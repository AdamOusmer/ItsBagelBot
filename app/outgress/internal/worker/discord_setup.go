// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"fmt"
	"strings"

	discapi "ItsBagelBot/app/outgress/internal/discord"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

const (
	overwriteRole          = 0
	permViewChannel  int64 = 1024
	permSendMessages int64 = 2048
)

// GuildSetupResult is the snowflakes the dashboard writes into the Discord
// module blob after a successful fill. Outgress does not write modules.
type GuildSetupResult struct {
	GuildID          string
	LiveChannelID    string
	ClipsChannelID   string
	WelcomeChannelID string
	AlertsChannelID  string
	VoiceHubID       string
	LiveRoleID       string
	ModsRoleID       string
	RegularsRoleID   string
	MemberRoleID     string
	Refused          string // non-empty when the guild looks lived-in
}

// SetupGuild fills the Bagel community template into an existing guild.
// Owner/Administrator is the dashboard's job (OAuth). We refuse a living
// community so we do not wreck someone's home. broadcasterID is the Twitch
// user id written into the guild reverse index so dingress can find the
// board from a Discord snowflake; empty skips the index write.
func (w *Worker) SetupGuild(ctx context.Context, guildID, everyoneRoleID, broadcasterID string) (GuildSetupResult, error) {
	var out GuildSetupResult
	guild, ok := w.discord.(discordGuildAPI)
	if !ok || w.discord == nil {
		return out, fmt.Errorf("discord client unavailable")
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return out, fmt.Errorf("missing guild id")
	}
	if everyoneRoleID == "" {
		everyoneRoleID = guildID // Discord: @everyone role id equals guild id
	}

	existing, err := guild.ListGuildChannels(ctx, guildID)
	if err != nil {
		return out, err
	}
	if len(existing) >= ddiscord.LivingCommunityMinChannels {
		out.GuildID = guildID
		out.Refused = "this server already has a layout; pick Connect existing instead of Set up this server"
		w.bindGuild(ctx, guildID, broadcasterID)
		return out, nil
	}

	roles, err := guild.ListGuildRoles(ctx, guildID)
	if err != nil {
		return out, err
	}
	roleByName := map[string]string{}
	for _, r := range roles {
		roleByName[strings.ToLower(r.Name)] = r.ID
	}
	for _, spec := range ddiscord.CommunityRoles() {
		id := roleByName[strings.ToLower(spec.Name)]
		if id == "" {
			created, err := guild.CreateRole(ctx, guildID, discapi.RoleCreate{
				Name: spec.Name, Hoist: spec.Hoist, Mentionable: spec.Mentionable,
			})
			if err != nil {
				return out, err
			}
			id = created.ID
			roleByName[strings.ToLower(spec.Name)] = id
		}
		switch spec.Name {
		case "Live":
			out.LiveRoleID = id
		case "Mods":
			out.ModsRoleID = id
		case "Regulars":
			out.RegularsRoleID = id
		case "Member":
			out.MemberRoleID = id
		}
	}

	chanByName := map[string]discapi.Snowflake{}
	for _, ch := range existing {
		chanByName[strings.ToLower(ch.Name)] = ch
	}
	parentID := map[string]string{}
	for _, spec := range ddiscord.CommunityChannels() {
		if spec.Type == ddiscord.ChannelCategory {
			id := chanByName[strings.ToLower(spec.Name)].ID
			if id == "" {
				created, err := guild.CreateChannel(ctx, guildID, discapi.ChannelCreate{
					Name: spec.Name, Type: spec.Type,
					PermissionOverwrites: overwrites(spec, everyoneRoleID),
				})
				if err != nil {
					return out, err
				}
				id = created.ID
				chanByName[strings.ToLower(spec.Name)] = created
			}
			parentID[spec.Name] = id
		}
	}
	for _, spec := range ddiscord.CommunityChannels() {
		if spec.Type == ddiscord.ChannelCategory {
			continue
		}
		id := chanByName[strings.ToLower(spec.Name)].ID
		if id == "" {
			created, err := guild.CreateChannel(ctx, guildID, discapi.ChannelCreate{
				Name:                 spec.Name,
				Type:                 spec.Type,
				Topic:                spec.Topic,
				ParentID:             parentID[spec.Parent],
				PermissionOverwrites: overwrites(spec, everyoneRoleID),
			})
			if err != nil {
				return out, err
			}
			id = created.ID
			chanByName[strings.ToLower(spec.Name)] = created
		}
		switch spec.Bind {
		case "live":
			out.LiveChannelID = id
		case "clips":
			out.ClipsChannelID = id
		case "welcome":
			out.WelcomeChannelID = id
		case "alerts":
			out.AlertsChannelID = id
		case "voice":
			out.VoiceHubID = id
		}
	}
	out.GuildID = guildID
	w.bindGuild(ctx, guildID, broadcasterID)
	return out, nil
}

func (w *Worker) bindGuild(ctx context.Context, guildID, broadcasterID string) {
	if w.discordKV == nil || guildID == "" || broadcasterID == "" {
		return
	}
	_ = w.discordKV.PutGuild(ctx, guildID, broadcasterID)
}

func overwrites(spec ddiscord.ChannelSpec, everyone string) []discapi.PermissionOverwrite {
	if !spec.Staff && !spec.ReadOnly {
		return nil
	}
	deny := int64(0)
	if spec.Staff {
		deny = permViewChannel
	} else if spec.ReadOnly {
		deny = permSendMessages
	}
	return []discapi.PermissionOverwrite{{
		ID:    everyone,
		Type:  overwriteRole,
		Allow: "0",
		Deny:  fmt.Sprintf("%d", deny),
	}}
}
