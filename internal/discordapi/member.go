// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"net/http"
	"net/url"
)

// MemberUser is the user object nested in a guild member, trimmed to what a
// staff check or a transcript line needs.
type MemberUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

// GuildMemberInfo is GET /guilds/{guild}/members/{user}.
//
// Named Info rather than GuildMember because GuildMember already ADDRESSES
// a member (the ban/kick pair take one); this is what Discord answers with.
type GuildMemberInfo struct {
	Roles []string   `json:"roles"`
	Nick  string     `json:"nick"`
	User  MemberUser `json:"user"`
}

// GetGuildMember reads one member's roles and display fields. This is the
// only way to learn a member's roles outside an event that happens to carry
// them: a button press names the presser, not what they hold.
func (c *Client) GetGuildMember(ctx context.Context, m GuildMember) (GuildMemberInfo, error) {
	var out GuildMemberInfo
	err := c.doInto(ctx, request{method: http.MethodGet, path: memberPath(m)}, &out)
	return out, err
}

func memberPath(m GuildMember) string {
	return "/guilds/" + url.PathEscape(m.GuildID) + "/members/" + url.PathEscape(m.UserID)
}

// ChannelInfo is one channel as GET /guilds/{id}/channels returns it,
// including the two fields Snowflake omits: its category and its existing
// overwrites. Lockdown needs both -- the parent to pick the channels in the
// configured categories, and the overwrites because Discord's overwrite
// write is a REPLACE, so denying SEND without reading the current bits
// would silently drop every other permission on that role.
type ChannelInfo struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Type                 int                   `json:"type"`
	ParentID             string                `json:"parent_id"`
	PermissionOverwrites []PermissionOverwrite `json:"permission_overwrites"`
}

// ListGuildChannelsFull is ListGuildChannels with the parent and overwrites
// kept. Separate from ListGuildChannels rather than widening it: the fill
// lists 20+ channels on every setup and has no use for either field, and
// Snowflake is the shape the store and the dashboard layout already speak.
func (c *Client) ListGuildChannelsFull(ctx context.Context, guild Guild) ([]ChannelInfo, error) {
	var out []ChannelInfo
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "/channels"}, &out)
	return out, err
}

// GuildVerificationHighest is Discord's VERY_HIGH verification level:
// members must have a verified phone number. It is the strongest lever a
// bot has against a raid -- one PATCH stops every new account at the door,
// where banning attackers one at a time competes with everything else for
// the same ~50 req/s token budget.
const GuildVerificationHighest = 4

// GuildPatch is PATCH /guilds/{id}, limited to the fields Bagel sets.
// VerificationLevel is a pointer so "leave it alone" stays distinguishable
// from level 0 (NONE), which is a real setting a guild may want back.
type GuildPatch struct {
	Guild             Guild
	VerificationLevel *int
}

// ModifyGuild applies a guild-level setting change. Requires MANAGE_GUILD.
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

// ChannelOverwrite is PUT /channels/{channel}/permissions/{overwrite}.
type ChannelOverwrite struct {
	ChannelID string
	Overwrite PermissionOverwrite
}

// SetChannelOverwrite writes ONE overwrite on a channel, leaving the others
// untouched. This is why lockdown does not go through ModifyChannel: that
// endpoint's permission_overwrites field replaces the whole array, so
// editing one role there means resending every other overwrite exactly, and
// getting that wrong opens a private channel to the server.
func (c *Client) SetChannelOverwrite(ctx context.Context, o ChannelOverwrite) error {
	path := "/channels/" + url.PathEscape(o.ChannelID) + "/permissions/" + url.PathEscape(o.Overwrite.ID)
	return c.do(ctx, request{method: http.MethodPut, path: path, body: o.Overwrite})
}
