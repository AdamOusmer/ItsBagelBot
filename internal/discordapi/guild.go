// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

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

// SendPanel posts an embed with buttons (the ticket desk).
func (c *Client) SendPanel(ctx context.Context, post EmbedPost, buttons []Button) (Message, error) {
	body := map[string]any{"embeds": []domain.Embed{post.Embed}}
	if post.Content != "" {
		body["content"] = post.Content
	}
	if len(buttons) > 0 {
		body["components"] = []map[string]any{{
			"type":       1,
			"components": buttonRows(buttons),
		}}
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
	return c.InteractionCallback(ctx, Callback{
		Interaction: reply.Interaction,
		Type:        4,
		Content:     reply.Content,
	})
}

// Button is one Discord message-component button.
type Button struct {
	Style    int
	Label    string
	CustomID string
}

// Callback is a type-4 interaction response that may carry embeds and buttons.
type Callback struct {
	Interaction Interaction
	Type        int
	Content     string
	Embeds      []domain.Embed
	Buttons     []Button
	Ephemeral   bool
}

// InteractionCallback posts /interactions/{id}/{token}/callback.
func (c *Client) InteractionCallback(ctx context.Context, cb Callback) error {
	if cb.Type == 0 {
		cb.Type = 4
	}
	data := map[string]any{}
	if cb.Content != "" {
		data["content"] = cb.Content
	}
	if len(cb.Embeds) > 0 {
		data["embeds"] = cb.Embeds
	}
	if len(cb.Buttons) > 0 {
		data["components"] = []map[string]any{{
			"type":       1,
			"components": buttonRows(cb.Buttons),
		}}
	}
	if cb.Ephemeral {
		data["flags"] = 64
	}
	body := map[string]any{"type": cb.Type, "data": data}
	in := cb.Interaction
	path := "/interactions/" + url.PathEscape(in.ID) + "/" + url.PathEscape(in.Token) + "/callback"
	return c.do(ctx, request{method: http.MethodPost, path: path, body: body})
}

func buttonRows(buttons []Button) []map[string]any {
	out := make([]map[string]any, 0, len(buttons))
	for _, b := range buttons {
		style := b.Style
		if style == 0 {
			style = 1
		}
		out = append(out, map[string]any{
			"type": 2, "style": style, "label": b.Label, "custom_id": b.CustomID,
		})
	}
	return out
}

// VoiceMove is PATCH /guilds/{guild}/members/{user} with a channel_id.
type VoiceMove struct {
	GuildID   string
	UserID    string
	ChannelID string
}

// MoveMember puts a member into a voice channel (empty ChannelID disconnects).
func (c *Client) MoveMember(ctx context.Context, move VoiceMove) error {
	body := map[string]any{"channel_id": nil}
	if move.ChannelID != "" {
		body["channel_id"] = move.ChannelID
	}
	path := "/guilds/" + url.PathEscape(move.GuildID) + "/members/" + url.PathEscape(move.UserID)
	return c.do(ctx, request{method: http.MethodPatch, path: path, body: body})
}

// ChannelPatch is PATCH /channels/{id}.
type ChannelPatch struct {
	ID                   string
	Name                 string
	UserLimit            int
	PermissionOverwrites []PermissionOverwrite
}

// ModifyChannel updates a channel's name, user limit, or overwrites.
func (c *Client) ModifyChannel(ctx context.Context, patch ChannelPatch) error {
	body := map[string]any{}
	if patch.Name != "" {
		body["name"] = patch.Name
	}
	if patch.UserLimit > 0 {
		body["user_limit"] = patch.UserLimit
	}
	if patch.PermissionOverwrites != nil {
		body["permission_overwrites"] = patch.PermissionOverwrites
	}
	return c.do(ctx, request{method: http.MethodPatch, path: "/channels/" + url.PathEscape(patch.ID), body: body})
}

// MemberTimeout is PATCH /guilds/{guild}/members/{user} communication_disabled_until.
type MemberTimeout struct {
	GuildID  string
	UserID   string
	UntilISO string
	Reason   string
}

// TimeoutMember applies Discord's timeout. Empty UntilISO clears it.
func (c *Client) TimeoutMember(ctx context.Context, t MemberTimeout) error {
	body := map[string]any{"communication_disabled_until": nil}
	if t.UntilISO != "" {
		body["communication_disabled_until"] = t.UntilISO
	}
	path := "/guilds/" + url.PathEscape(t.GuildID) + "/members/" + url.PathEscape(t.UserID)
	return c.do(ctx, request{method: http.MethodPatch, path: path, body: body})
}

// KickMember is DELETE /guilds/{guild}/members/{user}.
func (c *Client) KickMember(ctx context.Context, m GuildMember) error {
	path := "/guilds/" + url.PathEscape(m.GuildID) + "/members/" + url.PathEscape(m.UserID)
	return c.do(ctx, request{method: http.MethodDelete, path: path})
}

// BanMember is PUT /guilds/{guild}/bans/{user}.
func (c *Client) BanMember(ctx context.Context, m GuildMember) error {
	path := "/guilds/" + url.PathEscape(m.GuildID) + "/bans/" + url.PathEscape(m.UserID)
	return c.do(ctx, request{method: http.MethodPut, path: path, body: map[string]any{"delete_message_seconds": 0}})
}

// GuildMember addresses one user in one guild without a role.
type GuildMember struct {
	GuildID string
	UserID  string
}

// Purge is bulk-delete of 2–100 messages.
type Purge struct {
	ChannelID  string
	MessageIDs []string
}

// BulkDeleteMessages is POST /channels/{id}/messages/bulk-delete.
func (c *Client) BulkDeleteMessages(ctx context.Context, p Purge) error {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/channels/" + url.PathEscape(p.ChannelID) + "/messages/bulk-delete",
		body:   map[string]any{"messages": p.MessageIDs},
	})
}

// ListMessages is GET /channels/{id}/messages?limit=n.
func (c *Client) ListMessages(ctx context.Context, channelID string, limit int) ([]Snowflake, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []Snowflake
	path := "/channels/" + url.PathEscape(channelID) + "/messages?limit=" + itoa(limit)
	err := c.doInto(ctx, request{method: http.MethodGet, path: path}, &out)
	return out, err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// AppCommand is one slash-command definition for bulk overwrite.
type AppCommand struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Options     []AppCommandOption `json:"options,omitempty"`
}

// AppCommandOption is one slash option or subcommand.
type AppCommandOption struct {
	Type        int                `json:"type"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Required    bool               `json:"required,omitempty"`
	Options     []AppCommandOption `json:"options,omitempty"`
}

// BulkOverwriteCommands is PUT /applications/{id}/commands.
func (c *Client) BulkOverwriteCommands(ctx context.Context, appID string, cmds []AppCommand) error {
	path := "/applications/" + url.PathEscape(appID) + "/commands"
	return c.do(ctx, request{method: http.MethodPut, path: path, body: cmds})
}
