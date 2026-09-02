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

// EmbedPost is one embed (and optional content) to send into a channel.
type EmbedPost struct {
	ChannelID string
	Content   string
	Embed     domain.Embed
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

func guildPath(guildID, rest string) string { return "/guilds/" + url.PathEscape(guildID) + rest }

func channelPath(channelID, rest string) string {
	return "/channels/" + url.PathEscape(channelID) + rest
}

func messagePath(m Message) string {
	return channelPath(m.ChannelID, "/messages/"+url.PathEscape(m.ID))
}

func memberRolePath(r MemberRole) string {
	return guildPath(r.GuildID, "/members/"+url.PathEscape(r.UserID)+"/roles/"+url.PathEscape(r.RoleID))
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
	req := request{method: http.MethodPost, path: channelPath(post.ChannelID, "/messages"), body: body}
	if err := c.doInto(ctx, req, &ref); err != nil {
		return Message{}, err
	}
	if ref.ID == "" {
		return Message{}, ErrNoMessageID
	}
	return Message{ChannelID: post.ChannelID, ID: ref.ID}, nil
}

// EditMessage patches content (and optionally replaces embeds).
func (c *Client) EditMessage(ctx context.Context, m Message, content string, embeds []domain.Embed) error {
	body := map[string]any{"content": content}
	if embeds != nil {
		body["embeds"] = embeds
	}
	return c.do(ctx, request{method: http.MethodPatch, path: messagePath(m), body: body})
}

// DeleteMessage removes one message.
func (c *Client) DeleteMessage(ctx context.Context, m Message) error {
	return c.do(ctx, request{method: http.MethodDelete, path: messagePath(m)})
}

// CreateChannel creates one guild channel and returns its snowflake.
func (c *Client) CreateChannel(ctx context.Context, guildID string, ch ChannelCreate) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodPost, path: guildPath(guildID, "/channels"), body: ch}, &out)
	return out, err
}

// DeleteChannel removes a voice clone (or any channel the bot created).
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	return c.do(ctx, request{method: http.MethodDelete, path: channelPath(channelID, "")})
}

// CreateRole creates a guild role.
func (c *Client) CreateRole(ctx context.Context, guildID string, role RoleCreate) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodPost, path: guildPath(guildID, "/roles"), body: role}, &out)
	return out, err
}

// AddMemberRole grants one role.
func (c *Client) AddMemberRole(ctx context.Context, r MemberRole) error {
	return c.do(ctx, request{method: http.MethodPut, path: memberRolePath(r), body: struct{}{}})
}

// RemoveMemberRole revokes one role.
func (c *Client) RemoveMemberRole(ctx context.Context, r MemberRole) error {
	return c.do(ctx, request{method: http.MethodDelete, path: memberRolePath(r)})
}

// ListGuildChannels returns the guild's channels (for matching names on fill).
func (c *Client) ListGuildChannels(ctx context.Context, guildID string) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guildPath(guildID, "/channels")}, &out)
	return out, err
}

// ListGuildRoles returns the guild's roles (for matching names on fill).
func (c *Client) ListGuildRoles(ctx context.Context, guildID string) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guildPath(guildID, "/roles")}, &out)
	return out, err
}

// GetGuild returns the guild name and channel count (living-community check).
func (c *Client) GetGuild(ctx context.Context, guildID string) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guildPath(guildID, "?with_counts=false")}, &out)
	return out, err
}

// InteractionRespond answers a slash command (type 4 channel message).
func (c *Client) InteractionRespond(ctx context.Context, in Interaction, content string) error {
	body := map[string]any{
		"type": 4,
		"data": map[string]any{"content": content},
	}
	path := "/interactions/" + url.PathEscape(in.ID) + "/" + url.PathEscape(in.Token) + "/callback"
	return c.do(ctx, request{method: http.MethodPost, path: path, body: body})
}
