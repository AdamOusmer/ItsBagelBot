// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"context"
	"net/http"
	"net/url"

	domain "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
)

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

// MessageRef is the created-message id we store to edit go-live posts.
type MessageRef struct {
	ID string `json:"id"`
}

// SendEmbed posts one embed (and optional content) and returns the message id
// so a later stream.offline can edit it.
func (c *Client) SendEmbed(ctx context.Context, channelID, content string, embed domain.Embed) (string, error) {
	body := map[string]any{"embeds": []domain.Embed{embed}}
	if content != "" {
		body["content"] = content
	}
	raw, err := c.doBytes(ctx, http.MethodPost, "/channels/"+url.PathEscape(channelID)+"/messages", body)
	if err != nil {
		return "", err
	}
	var ref MessageRef
	_ = codec.Unmarshal(raw, &ref)
	return ref.ID, nil
}

// EditMessage patches content (and optionally replaces embeds).
func (c *Client) EditMessage(ctx context.Context, channelID, messageID, content string, embeds []domain.Embed) error {
	body := map[string]any{"content": content}
	if embeds != nil {
		body["embeds"] = embeds
	}
	return c.do(ctx, http.MethodPatch, "/channels/"+url.PathEscape(channelID)+"/messages/"+url.PathEscape(messageID), body)
}

// DeleteMessage removes one message.
func (c *Client) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	_, err := c.doBytes(ctx, http.MethodDelete, "/channels/"+url.PathEscape(channelID)+"/messages/"+url.PathEscape(messageID), nil)
	return err
}

// CreateChannel creates one guild channel and returns its snowflake.
func (c *Client) CreateChannel(ctx context.Context, guildID string, ch ChannelCreate) (Snowflake, error) {
	raw, err := c.doBytes(ctx, http.MethodPost, "/guilds/"+url.PathEscape(guildID)+"/channels", ch)
	if err != nil {
		return Snowflake{}, err
	}
	var out Snowflake
	_ = codec.Unmarshal(raw, &out)
	return out, nil
}

// DeleteChannel removes a voice clone (or any channel the bot created).
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	_, err := c.doBytes(ctx, http.MethodDelete, "/channels/"+url.PathEscape(channelID), nil)
	return err
}

// CreateRole creates a guild role.
func (c *Client) CreateRole(ctx context.Context, guildID string, role RoleCreate) (Snowflake, error) {
	raw, err := c.doBytes(ctx, http.MethodPost, "/guilds/"+url.PathEscape(guildID)+"/roles", role)
	if err != nil {
		return Snowflake{}, err
	}
	var out Snowflake
	_ = codec.Unmarshal(raw, &out)
	return out, nil
}

// AddMemberRole grants one role.
func (c *Client) AddMemberRole(ctx context.Context, guildID, userID, roleID string) error {
	return c.do(ctx, http.MethodPut, "/guilds/"+url.PathEscape(guildID)+"/members/"+url.PathEscape(userID)+"/roles/"+url.PathEscape(roleID), struct{}{})
}

// RemoveMemberRole revokes one role.
func (c *Client) RemoveMemberRole(ctx context.Context, guildID, userID, roleID string) error {
	_, err := c.doBytes(ctx, http.MethodDelete, "/guilds/"+url.PathEscape(guildID)+"/members/"+url.PathEscape(userID)+"/roles/"+url.PathEscape(roleID), nil)
	return err
}

// ListGuildChannels returns the guild's channels (for matching names on fill).
func (c *Client) ListGuildChannels(ctx context.Context, guildID string) ([]Snowflake, error) {
	raw, err := c.doBytes(ctx, http.MethodGet, "/guilds/"+url.PathEscape(guildID)+"/channels", nil)
	if err != nil {
		return nil, err
	}
	var out []Snowflake
	if err := codec.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListGuildRoles returns the guild's roles (for matching names on fill).
func (c *Client) ListGuildRoles(ctx context.Context, guildID string) ([]Snowflake, error) {
	raw, err := c.doBytes(ctx, http.MethodGet, "/guilds/"+url.PathEscape(guildID)+"/roles", nil)
	if err != nil {
		return nil, err
	}
	var out []Snowflake
	if err := codec.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetGuild returns the guild name and channel count (living-community check).
func (c *Client) GetGuild(ctx context.Context, guildID string) (Snowflake, error) {
	raw, err := c.doBytes(ctx, http.MethodGet, "/guilds/"+url.PathEscape(guildID)+"?with_counts=false", nil)
	if err != nil {
		return Snowflake{}, err
	}
	var out Snowflake
	_ = codec.Unmarshal(raw, &out)
	return out, nil
}

// InteractionRespond answers a slash command (type 4 channel message).
func (c *Client) InteractionRespond(ctx context.Context, interactionID, token, content string) error {
	body := map[string]any{
		"type": 4,
		"data": map[string]any{"content": content},
	}
	return c.do(ctx, http.MethodPost, "/interactions/"+url.PathEscape(interactionID)+"/"+url.PathEscape(token)+"/callback", body)
}

func (c *Client) doBytes(ctx context.Context, method, path string, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = codec.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	return c.callBytes(ctx, method, path, payload)
}
