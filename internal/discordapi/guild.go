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

var ErrNoMessageID = errors.New("discord: created message carried no id")

type Snowflake struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Type              int    `json:"type,omitempty"`
	Managed           bool   `json:"managed,omitempty"`
	Position          int    `json:"position,omitempty"`
	VerificationLevel int    `json:"verification_level,omitempty"`
}

type PermissionOverwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

type ChannelCreate struct {
	Name                 string                `json:"name"`
	Type                 int                   `json:"type"`
	Topic                string                `json:"topic,omitempty"`
	ParentID             string                `json:"parent_id,omitempty"`
	PermissionOverwrites []PermissionOverwrite `json:"permission_overwrites,omitempty"`
}

type RoleCreate struct {
	Name        string `json:"name"`
	Hoist       bool   `json:"hoist,omitempty"`
	Mentionable bool   `json:"mentionable,omitempty"`
	Color       int    `json:"color,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

type Message struct {
	ChannelID string
	ID        string
}

type MessagePatch struct {
	Content string
	Embeds  []domain.Embed
}

type EmbedPost struct {
	ChannelID string
	Content   string
	Embed     domain.Embed
}

type Guild struct {
	ID string
}

type GuildChannel struct {
	Guild Guild
	Spec  ChannelCreate
}

type GuildRole struct {
	Guild Guild
	Spec  RoleCreate
}

type MemberRole struct {
	GuildID string
	UserID  string
	RoleID  string
}

type Interaction struct {
	ID    string
	Token string
}

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

func (c *Client) EditMessage(ctx context.Context, m Message, patch MessagePatch) error {
	body := map[string]any{"content": patch.Content}
	if patch.Embeds != nil {
		body["embeds"] = patch.Embeds
	}
	return c.do(ctx, request{method: http.MethodPatch, path: m.path(), body: body})
}

func (c *Client) DeleteMessage(ctx context.Context, m Message) error {
	return c.do(ctx, request{method: http.MethodDelete, path: m.path()})
}

func (c *Client) CreateChannel(ctx context.Context, ch GuildChannel) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodPost, path: ch.Guild.path() + "/channels", body: ch.Spec}, &out)
	return out, err
}

func (c *Client) DeleteChannel(ctx context.Context, ch Snowflake) error {
	return c.do(ctx, request{method: http.MethodDelete, path: "/channels/" + url.PathEscape(ch.ID)})
}

func (c *Client) CreateRole(ctx context.Context, role GuildRole) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodPost, path: role.Guild.path() + "/roles", body: role.Spec}, &out)
	return out, err
}

func (c *Client) AddMemberRole(ctx context.Context, r MemberRole) error {
	return c.do(ctx, request{method: http.MethodPut, path: r.path(), body: struct{}{}})
}

func (c *Client) RemoveMemberRole(ctx context.Context, r MemberRole) error {
	return c.do(ctx, request{method: http.MethodDelete, path: r.path()})
}

func (c *Client) RemoveMemberRoleWithReason(ctx context.Context, r MemberRole, reason string) error {
	return c.do(ctx, request{method: http.MethodDelete, path: r.path(), reason: reason})
}

func (c *Client) ListGuildChannels(ctx context.Context, guild Guild) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "/channels"}, &out)
	return out, err
}

func (c *Client) ListGuildRoles(ctx context.Context, guild Guild) ([]Snowflake, error) {
	var out []Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "/roles"}, &out)
	return out, err
}

func (c *Client) GetGuild(ctx context.Context, guild Guild) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "?with_counts=false"}, &out)
	return out, err
}

type GuildInfo struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	Icon                     string `json:"icon"`
	ApproximateMemberCount   int    `json:"approximate_member_count"`
	ApproximatePresenceCount int    `json:"approximate_presence_count"`
}

func (g GuildInfo) IconURL() string {
	if g.Icon == "" || g.ID == "" {
		return ""
	}
	return "https://cdn.discordapp.com/icons/" + g.ID + "/" + g.Icon + ".png"
}

func (c *Client) GetGuildWithCounts(ctx context.Context, guild Guild) (GuildInfo, error) {
	var out GuildInfo
	err := c.doInto(ctx, request{method: http.MethodGet, path: guild.path() + "?with_counts=true"}, &out)
	return out, err
}

type Invite struct {
	Code  string     `json:"code"`
	Guild *Snowflake `json:"guild"`
}

func (c *Client) GetInvite(ctx context.Context, code string) (Invite, error) {
	var out Invite
	err := c.doInto(ctx, request{method: http.MethodGet, path: "/invites/" + url.PathEscape(code) + "?with_counts=false"}, &out)
	return out, err
}

func (c *Client) InteractionRespond(ctx context.Context, reply InteractionReply) error {
	return c.InteractionCallback(ctx, Callback{
		Interaction: reply.Interaction,
		Type:        4,
		Content:     reply.Content,
	})
}

type Button struct {
	Style    int
	Label    string
	CustomID string
}

type Callback struct {
	Interaction Interaction
	Type        int
	Content     string
	Embeds      []domain.Embed
	Buttons     []Button
	Ephemeral   bool
}

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

type VoiceMove struct {
	GuildID   string
	UserID    string
	ChannelID string
}

func (c *Client) MoveMember(ctx context.Context, move VoiceMove) error {
	body := map[string]any{"channel_id": nil}
	if move.ChannelID != "" {
		body["channel_id"] = move.ChannelID
	}
	path := "/guilds/" + url.PathEscape(move.GuildID) + "/members/" + url.PathEscape(move.UserID)
	return c.do(ctx, request{method: http.MethodPatch, path: path, body: body})
}

type ChannelPatch struct {
	ID                   string
	Name                 string
	UserLimit            int
	PermissionOverwrites []PermissionOverwrite
	ParentID             *string
}

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
	addParent(body, patch.ParentID)
	return c.do(ctx, request{method: http.MethodPatch, path: "/channels/" + url.PathEscape(patch.ID), body: body})
}

func addParent(body map[string]any, parentID *string) {
	if parentID == nil {
		return
	}
	if *parentID == "" {
		body["parent_id"] = nil
		return
	}
	body["parent_id"] = *parentID
}

type MemberTimeout struct {
	GuildID  string
	UserID   string
	UntilISO string
	Reason   string
}

func (c *Client) TimeoutMember(ctx context.Context, t MemberTimeout) error {
	body := map[string]any{"communication_disabled_until": nil}
	if t.UntilISO != "" {
		body["communication_disabled_until"] = t.UntilISO
	}
	path := "/guilds/" + url.PathEscape(t.GuildID) + "/members/" + url.PathEscape(t.UserID)
	return c.do(ctx, request{method: http.MethodPatch, path: path, body: body})
}

func (c *Client) KickMember(ctx context.Context, m GuildMember) error {
	path := "/guilds/" + url.PathEscape(m.GuildID) + "/members/" + url.PathEscape(m.UserID)
	return c.do(ctx, request{method: http.MethodDelete, path: path})
}

func (c *Client) BanMember(ctx context.Context, m GuildMember) error {
	path := "/guilds/" + url.PathEscape(m.GuildID) + "/bans/" + url.PathEscape(m.UserID)
	return c.do(ctx, request{method: http.MethodPut, path: path, body: map[string]any{"delete_message_seconds": 0}})
}

type GuildMember struct {
	GuildID string
	UserID  string
}

type Purge struct {
	ChannelID  string
	MessageIDs []string
}

func (c *Client) BulkDeleteMessages(ctx context.Context, p Purge) error {
	return c.do(ctx, request{
		method: http.MethodPost,
		path:   "/channels/" + url.PathEscape(p.ChannelID) + "/messages/bulk-delete",
		body:   map[string]any{"messages": p.MessageIDs},
	})
}

type MessageQuery struct {
	ChannelID string
	Limit     int
}

func (c *Client) ListMessages(ctx context.Context, q MessageQuery) ([]Snowflake, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	var out []Snowflake
	path := "/channels/" + url.PathEscape(q.ChannelID) + "/messages?limit=" + itoa(limit)
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

func (c *Client) GetCurrentApplication(ctx context.Context) (Snowflake, error) {
	var out Snowflake
	err := c.doInto(ctx, request{method: http.MethodGet, path: "/oauth2/applications/@me"}, &out)
	return out, err
}

type Followup struct {
	ApplicationID string
	Token         string
	Content       string
	Embeds        []domain.Embed
	Buttons       []Button
	Ephemeral     bool
}

func (c *Client) InteractionFollowup(ctx context.Context, f Followup) error {
	body := map[string]any{}
	if f.Content != "" {
		body["content"] = f.Content
	}
	if len(f.Embeds) > 0 {
		body["embeds"] = f.Embeds
	}
	if len(f.Buttons) > 0 {
		body["components"] = []map[string]any{{
			"type":       1,
			"components": buttonRows(f.Buttons),
		}}
	}
	if f.Ephemeral {
		body["flags"] = 64
	}
	path := "/webhooks/" + url.PathEscape(f.ApplicationID) + "/" + url.PathEscape(f.Token)
	return c.do(ctx, request{method: http.MethodPost, path: path, body: body})
}

type AppCommand struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Options     []AppCommandOption `json:"options,omitempty"`
}

type AppCommandOption struct {
	Type        int                `json:"type"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Required    bool               `json:"required,omitempty"`
	Options     []AppCommandOption `json:"options,omitempty"`
}

type CommandCatalog struct {
	ApplicationID string
	Commands      []AppCommand
}

func (c *Client) BulkOverwriteCommands(ctx context.Context, cat CommandCatalog) error {
	path := "/applications/" + url.PathEscape(cat.ApplicationID) + "/commands"
	return c.do(ctx, request{method: http.MethodPut, path: path, body: cat.Commands})
}

type CurrentMember struct {
	GuildID       string
	Nick          *string
	AvatarDataURI *string
}

type modifyCurrentMemberBody struct {
	Nick   *string `json:"nick"`
	Avatar *string `json:"avatar"`
}

func (c *Client) ModifyCurrentMember(ctx context.Context, m CurrentMember) error {
	return c.do(ctx, request{
		method: http.MethodPatch,
		path:   "/guilds/" + m.GuildID + "/members/@me",
		body:   modifyCurrentMemberBody{Nick: m.Nick, Avatar: m.AvatarDataURI},
	})
}
