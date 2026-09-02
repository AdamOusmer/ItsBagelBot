// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	domain "ItsBagelBot/internal/domain/discord"
)

// ErrNoMessageID means Discord accepted a create but the echo carried no id,
// so a later edit (go-offline) has nothing to target.
var ErrNoMessageID = errors.New("discord: created message carried no id")

// Snowflake is the subset of a Discord object we persist.
type Snowflake struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type,omitempty"`
}

// PermissionOverwrite is a Discord channel overwrite.
type PermissionOverwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"` // 0 role, 1 member
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

// ChannelCreate is POST /guilds/{id}/channels.
type ChannelCreate struct {
	Name                 string                `json:"name"`
	Type                 int                   `json:"type"`
	Topic                string                `json:"topic,omitempty"`
	ParentID             string                `json:"parent_id,omitempty"`
	PermissionOverwrites []PermissionOverwrite `json:"permission_overwrites,omitempty"`
}

// RoleCreate is POST /guilds/{id}/roles.
type RoleCreate struct {
	Name        string `json:"name"`
	Hoist       bool   `json:"hoist,omitempty"`
	Mentionable bool   `json:"mentionable,omitempty"`
}

// Message addresses one message: the channel it lives in and its id. It is
// what the live store remembers so stream.offline can edit the go-live post.
type Message struct {
	ChannelID string
	ID        string
}

// MessagePatch is the body of PATCH /channels/{id}/messages/{id}.
type MessagePatch struct {
	Content string
	Embeds  []domain.Embed
}

// EmbedPost is one embed (and optional content) to send into a channel.
type EmbedPost struct {
	ChannelID string
	Content   string
	Embed     domain.Embed
}

// Guild is a Discord guild id. REST calls that target a guild take this
// instead of a bare string so the worker and client stay off primitive args.
type Guild struct {
	ID string
}

// GuildChannel is POST /guilds/{id}/channels.
type GuildChannel struct {
	Guild Guild
	Spec  ChannelCreate
}

// GuildRole is POST /guilds/{id}/roles.
type GuildRole struct {
	Guild Guild
	Spec  RoleCreate
}

// MemberRole addresses one role grant on one guild member.
type MemberRole struct {
	GuildID string
	UserID  string
	RoleID  string
}

// Interaction addresses one slash-command interaction callback.
type Interaction struct {
	ID    string
	Token string
}

// InteractionReply is the type-4 channel-message answer to a slash command.
type InteractionReply struct {
	Interaction Interaction
	Content     string
}

func (g Guild) path() string {
	return "/guilds/" + url.PathEscape(g.ID)
}

func (m Message) path() string {
	return "/channels/" + url.PathEscape(m.ChannelID) + "/messages/" + url.PathEscape(m.ID)
}

func (r MemberRole) path() string {
	return "/guilds/" + url.PathEscape(r.GuildID) + "/members/" + url.PathEscape(r.UserID) + "/roles/" + url.PathEscape(r.RoleID)
}

// SendEmbed posts one embed and returns the created message so a later
// stream.offline can edit it.
func (c *Client) SendEmbed(ctx context.Context, post EmbedPost) (Message, error) {
	body := map[string]any{"embeds": []domain.Embed{post.Embed}}
	if post.Content != "" {
		body["content"] = post.Content
	}
	var ref struct {
		ID string `json:"id"`
	}
	req := request{method: http.MethodPost, path: "/channels/" + url.PathEscape(post.ChannelID) + "/messages", body: body}
	if err := c.doInto(ctx, req, &ref); err != nil {
		return Message{}, err
	}
	if ref.ID == "" {
		return Message{}, ErrNoMessageID
	}
	return Message{ChannelID: post.ChannelID, ID: ref.ID}, nil
}

// EditMessage patches content (and optionally replaces embeds).
func (c *Client) EditMessage(ctx context.Context, m Message, patch MessagePatch) error {
	body := map[string]any{"content": patch.Content}
	if patch.Embeds != nil {
		body["embeds"] = patch.Embeds
	}
	return c.do(ctx, request{method: http.MethodPatch, path: m.path(), body: body})
}

// DeleteMessage removes one message.
func (c *Client) DeleteMessage(ctx context.Context, m Message) error {
	return c.do(ctx, request{method: http.MethodDelete, path: m.path()})
}

// CreateChannel creates one guild channel and returns its snowflake.
func (c *Client) CreateChannel(ctx context.Context, ch GuildChannel) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodPost, path: ch.Guild.path() + "/channels", body: ch.Spec}, &out)
	return out, err
}

// DeleteChannel removes a voice clone (or any channel the bot created).
func (c *Client) DeleteChannel(ctx context.Context, ch Snowflake) error {
	return c.do(ctx, request{method: http.MethodDelete, path: "/channels/" + url.PathEscape(ch.ID)})
}

// CreateRole creates a guild role.
func (c *Client) CreateRole(ctx context.Context, role GuildRole) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodPost, path: role.Guild.path() + "/roles", body: role.Spec}, &out)
	return out, err
}

// AddMemberRole grants one role.
func (c *Client) AddMemberRole(ctx context.Context, r MemberRole) error {
	return c.do(ctx, request{method: http.MethodPut, path: r.path(), body: struct{}{}})
}

// RemoveMemberRole revokes one role.
func (c *Client) RemoveMemberRole(ctx context.Context, r MemberRole) error {
	return c.do(ctx, request{method: http.MethodDelete, path: r.path()})
}

// ListGuildChannels returns the guild's channels (for matching names on fill).
func (c *Client) ListGuildChannels(ctx context.Context, guild Guild) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "/channels"}, &out)
	return out, err
}

// ListGuildRoles returns the guild's roles (for matching names on fill).
func (c *Client) ListGuildRoles(ctx context.Context, guild Guild) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "/roles"}, &out)
	return out, err
}

// GetGuild returns the guild name and channel count (living-community check).
func (c *Client) GetGuild(ctx context.Context, guild Guild) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "?with_counts=false"}, &out)
	return out, err
}

// InteractionRespond answers a slash command (type 4 channel message).
func (c *Client) InteractionRespond(ctx context.Context, reply InteractionReply) error {
	body := map[string]any{
		"type": 4,
		"data": map[string]any{"content": reply.Content},
	}
	in := reply.Interaction
	path := "/interactions/" + url.PathEscape(in.ID) + "/" + url.PathEscape(in.Token) + "/callback"
	return c.do(ctx, request{method: http.MethodPost, path: path, body: body})
}
