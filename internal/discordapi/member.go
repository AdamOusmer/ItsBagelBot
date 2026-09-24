// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"net/http"
	"net/url"
)

type MemberUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

type GuildMemberInfo struct {
	Roles []string   `json:"roles"`
	Nick  string     `json:"nick"`
	User  MemberUser `json:"user"`
}

func (c *Client) GetGuildMember(ctx context.Context, m GuildMember) (GuildMemberInfo, error) {
	var out GuildMemberInfo
	err := c.doInto(ctx, request{method: http.MethodGet, path: memberPath(m)}, &out)
	return out, err
}

func memberPath(m GuildMember) string {
	return "/guilds/" + url.PathEscape(m.GuildID) + "/members/" + url.PathEscape(m.UserID)
}

type ChannelInfo struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Type                 int                   `json:"type"`
	ParentID             string                `json:"parent_id"`
	PermissionOverwrites []PermissionOverwrite `json:"permission_overwrites"`
}

func (c *Client) ListGuildChannelsFull(ctx context.Context, guild Guild) ([]ChannelInfo, error) {
	var out []ChannelInfo
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "/channels"}, &out)
	return out, err
}

const GuildVerificationHighest = 4

type GuildPatch struct {
	Guild             Guild
	VerificationLevel *int
}

func (c *Client) ModifyGuild(ctx context.Context, patch GuildPatch) error {
	body := map[string]any{}
	if patch.VerificationLevel != nil {
		body["verification_level"] = *patch.VerificationLevel
	}
	if len(body) == 0 {
		return nil
	}
	return c.do(ctx, request{method: http.MethodPatch, path: patch.Guild.path(), body: body})
}

type ChannelOverwrite struct {
	ChannelID string
	Overwrite PermissionOverwrite
}

// Use this, not ModifyChannel: resending a stale overwrite list can expose a private channel.
func (c *Client) SetChannelOverwrite(ctx context.Context, o ChannelOverwrite) error {
	path := "/channels/" + url.PathEscape(o.ChannelID) + "/permissions/" + url.PathEscape(o.Overwrite.ID)
	return c.do(ctx, request{method: http.MethodPut, path: path, body: o.Overwrite})
}
