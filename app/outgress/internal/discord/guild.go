// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	domain "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
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

// MessageRef is the created-message id we store to edit go-live posts.
type MessageRef struct {
	ID string `json:"id"`
}

func guildPath(guildID, rest string) string { return "/guilds/" + url.PathEscape(guildID) + rest }

func channelPath(channelID, rest string) string {
	return "/channels/" + url.PathEscape(channelID) + rest
}

func messagePath(channelID, messageID string) string {
	return channelPath(channelID, "/messages/"+url.PathEscape(messageID))
}

func memberRolePath(guildID, userID, roleID string) string {
	return guildPath(guildID, "/members/"+url.PathEscape(userID)+"/roles/"+url.PathEscape(roleID))
}

// SendEmbed posts one embed (and optional content) and returns the message id
// so a later stream.offline can edit it.
func (c *Client) SendEmbed(ctx context.Context, channelID, content string, embed domain.Embed) (string, error) {
	body := map[string]any{"embeds": []domain.Embed{embed}}
	if content != "" {
		body["content"] = content
	}
	var ref MessageRef
	if err := c.doInto(ctx, http.MethodPost, channelPath(channelID, "/messages"), body, &ref); err != nil {
		return "", err
	}
	if ref.ID == "" {
		return "", ErrNoMessageID
	}
	return ref.ID, nil
}

// EditMessage patches content (and optionally replaces embeds).
func (c *Client) EditMessage(ctx context.Context, channelID, messageID, content string, embeds []domain.Embed) error {
	body := map[string]any{"content": content}
	if embeds != nil {
		body["embeds"] = embeds
	}
	return c.do(ctx, http.MethodPatch, messagePath(channelID, messageID), body)
}

// DeleteMessage removes one message.
func (c *Client) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	_, err := c.doBytes(ctx, http.MethodDelete, messagePath(channelID, messageID), nil)
	return err
}

// CreateChannel creates one guild channel and returns its snowflake.
func (c *Client) CreateChannel(ctx context.Context, guildID string, ch ChannelCreate) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, http.MethodPost, guildPath(guildID, "/channels"), ch, &out)
	return out, err
}

// DeleteChannel removes a voice clone (or any channel the bot created).
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	_, err := c.doBytes(ctx, http.MethodDelete, channelPath(channelID, ""), nil)
	return err
}

// CreateRole creates a guild role.
func (c *Client) CreateRole(ctx context.Context, guildID string, role RoleCreate) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, http.MethodPost, guildPath(guildID, "/roles"), role, &out)
	return out, err
}

// AddMemberRole grants one role.
func (c *Client) AddMemberRole(ctx context.Context, guildID, userID, roleID string) error {
	return c.do(ctx, http.MethodPut, memberRolePath(guildID, userID, roleID), struct{}{})
}

// RemoveMemberRole revokes one role.
func (c *Client) RemoveMemberRole(ctx context.Context, guildID, userID, roleID string) error {
	_, err := c.doBytes(ctx, http.MethodDelete, memberRolePath(guildID, userID, roleID), nil)
	return err
}

// ListGuildChannels returns the guild's channels (for matching names on fill).
func (c *Client) ListGuildChannels(ctx context.Context, guildID string) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, http.MethodGet, guildPath(guildID, "/channels"), nil, &out)
	return out, err
}

// ListGuildRoles returns the guild's roles (for matching names on fill).
func (c *Client) ListGuildRoles(ctx context.Context, guildID string) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, http.MethodGet, guildPath(guildID, "/roles"), nil, &out)
	return out, err
}

// GetGuild returns the guild name and channel count (living-community check).
func (c *Client) GetGuild(ctx context.Context, guildID string) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, http.MethodGet, guildPath(guildID, "?with_counts=false"), nil, &out)
	return out, err
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

// doInto runs the call and decodes the success body into out. A body that
// does not decode is an error, never a silently zero result: a zero id here
// used to strand go-live posts as LIVE forever.
func (c *Client) doInto(ctx context.Context, method, path string, body, out any) error {
	raw, err := c.doBytes(ctx, method, path, body)
	if err != nil {
		return err
	}
	if err := codec.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("discord: decode %s %s: %w", method, path, err)
	}
	return nil
}
