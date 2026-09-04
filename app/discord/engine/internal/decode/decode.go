// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package decode holds the gateway-payload shapes and small helpers every
// engine module decodes from ddiscord.Event.Raw, moved unchanged in spirit
// from app/dingress/internal/community/bot.go (userRef, memberEvent,
// voiceEvent, messageEvent, interactionEvent, decode[T], display/avatar
// helpers, the permission-bit check, and the overwrite builders). Ingress
// never decodes these -- it only lifts the three routing ids (see
// app/discord/ingress/internal/relay) -- so this is where the actual
// business shapes live now.
package decode

import (
	"strconv"

	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/pkg/codec"
)

const (
	PermAdmin    uint64 = 8
	PermKick     uint64 = 2
	PermBan      uint64 = 4
	PermModerate uint64 = 1 << 40
	PermConnect  int64  = 1048576
	PermView     int64  = 1024
	PermSend     int64  = 2048
)

// UserRef is one Discord user as it appears embedded in a gateway payload.
type UserRef struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Avatar     string `json:"avatar"`
	Bot        bool   `json:"bot"`
}

type MemberEvent struct {
	GuildID string  `json:"guild_id"`
	Nick    string  `json:"nick"`
	User    UserRef `json:"user"`
}

type VoiceEvent struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Member    struct {
		User UserRef `json:"user"`
	} `json:"member"`
}

type MessageEvent struct {
	ID        string  `json:"id"`
	GuildID   string  `json:"guild_id"`
	ChannelID string  `json:"channel_id"`
	Content   string  `json:"content"`
	Author    UserRef `json:"author"`
	// Member is the partial guild member Discord attaches to a
	// MESSAGE_CREATE/UPDATE dispatch. Unlike InteractionEvent.Member
	// below, Discord does NOT compute a "permissions" bitstring for this
	// partial member -- that field only exists on an interaction's
	// resolved member -- so a message author's moderator status is
	// checked by role membership (see HasRole) rather than CanMod's bit
	// math, which has nothing to operate on here.
	Member struct {
		Roles []string `json:"roles"`
	} `json:"member"`
}

type InteractionOption struct {
	Name    string              `json:"name"`
	Type    int                 `json:"type"`
	Value   codec.RawMessage    `json:"value"`
	Options []InteractionOption `json:"options"`
}

type InteractionEvent struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	Type  int    `json:"type"`
	Data  struct {
		Name     string              `json:"name"`
		CustomID string              `json:"custom_id"`
		Options  []InteractionOption `json:"options"`
	} `json:"data"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	Member    struct {
		User        UserRef `json:"user"`
		Permissions string  `json:"permissions"`
		Nick        string  `json:"nick"`
	} `json:"member"`
}

// Decode unmarshals raw into T. Modules call this on ddiscord.Event.Raw the
// same way community's handlers did before this split.
func Decode[T any](raw []byte) (T, error) {
	var v T
	err := codec.Unmarshal(raw, &v)
	return v, err
}

// CanMod reports whether a permission bitstring (Discord's string-encoded
// int64, as carried on an interaction's member.permissions) includes any
// moderation-capable permission.
func CanMod(permRaw string) bool {
	n, err := strconv.ParseUint(permRaw, 10, 64)
	if err != nil {
		return false
	}
	return n&(PermAdmin|PermKick|PermBan|PermModerate) != 0
}

// HasRole reports whether roleID is among roles. Used against a
// MessageEvent's Member.Roles to check a message author against a guild's
// configured role (e.g. Config.ModsRoleID, the same role ticket.go already
// trusts to grant mod-only channel access) -- see MessageEvent.Member's
// doc for why this, and not CanMod, is the message-path check. An empty
// roleID (a guild with no such role configured) never matches.
func HasRole(roles []string, roleID string) bool {
	if roleID == "" {
		return false
	}
	for _, r := range roles {
		if r == roleID {
			return true
		}
	}
	return false
}

// Mention formats a user id as a Discord mention.
func Mention(user UserRef) string { return "<@" + user.ID + ">" }

// Display is the display-name resolution input: nickname first, then global
// display name, then username, then the bare id.
type Display struct {
	User UserRef
	Nick string
}

func DisplayName(d Display) string {
	if d.Nick != "" {
		return d.Nick
	}
	if d.User.GlobalName != "" {
		return d.User.GlobalName
	}
	if d.User.Username != "" {
		return d.User.Username
	}
	return d.User.ID
}

func AvatarURL(user UserRef) string {
	if user.ID == "" {
		return ""
	}
	if user.Avatar == "" {
		return ""
	}
	return "https://cdn.discordapp.com/avatars/" + user.ID + "/" + user.Avatar + ".png"
}

// OverwriteSpec is one permission overwrite the ticket/voice-clone flows
// build before asking outgress to create a channel with it.
type OverwriteSpec struct {
	TargetID string
	Kind     int
	Bits     int64
}

func OverwriteAllow(spec OverwriteSpec) discordapi.PermissionOverwrite {
	return discordapi.PermissionOverwrite{ID: spec.TargetID, Type: spec.Kind, Allow: strconv.FormatInt(spec.Bits, 10), Deny: "0"}
}

func OverwriteDeny(spec OverwriteSpec) discordapi.PermissionOverwrite {
	return discordapi.PermissionOverwrite{ID: spec.TargetID, Type: spec.Kind, Allow: "0", Deny: strconv.FormatInt(spec.Bits, 10)}
}

// FirstSub returns the first slash sub-command option, or a zero value when
// none was sent.
func FirstSub(opts []InteractionOption) InteractionOption {
	if len(opts) == 0 {
		return InteractionOption{}
	}
	return opts[0]
}

// findOption returns the first option in opts named name. OptionString,
// OptionInt and OptionIntFrom each used to walk their own copy of this
// "find by name" loop (CodeScene: Code Duplication); it is written once
// here and shared.
func findOption(opts []InteractionOption, name string) (InteractionOption, bool) {
	for _, o := range opts {
		if o.Name == name {
			return o, true
		}
	}
	return InteractionOption{}, false
}

// OptionString reads a named string option out of sub's own options.
func OptionString(sub InteractionOption, name string) string {
	if o, ok := findOption(sub.Options, name); ok {
		return rawString(o.Value)
	}
	return ""
}

// OptionInt reads a named integer option out of sub's own options.
func OptionInt(sub InteractionOption, name string) int {
	return OptionIntFrom(sub.Options, name)
}

// OptionIntFrom reads a named integer option out of a top-level option list.
func OptionIntFrom(opts []InteractionOption, name string) int {
	o, ok := findOption(opts, name)
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(rawString(o.Value))
	return n
}

// OptionUser reads a named user-id option, searching nested sub-command
// options too (a slash command's user option can sit under a subcommand).
func OptionUser(opts []InteractionOption, name string) string {
	for _, o := range opts {
		if o.Name == name {
			return rawString(o.Value)
		}
		if s := OptionUser(o.Options, name); s != "" {
			return s
		}
	}
	return ""
}

// isQuotedJSONString reports whether s is a JSON string literal (a codec
// RawMessage that decoded to a quoted value rather than a bare number or
// identifier) -- the only shape rawString needs to strip the quotes from.
func isQuotedJSONString(s string) bool {
	return len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"'
}

func rawString(raw codec.RawMessage) string {
	s := string(raw)
	if isQuotedJSONString(s) {
		return s[1 : len(s)-1]
	}
	return s
}

// Clip truncates s to at most n bytes, appending an ellipsis when it does.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// HasAnyRole reports whether any of roleIDs is among roles. Callers pass
// Config.StaffRoleIDs so every staff check asks the same question; see that
// method for why the set is defined in one place.
func HasAnyRole(roles []string, roleIDs []string) bool {
	for _, id := range roleIDs {
		if HasRole(roles, id) {
			return true
		}
	}
	return false
}
