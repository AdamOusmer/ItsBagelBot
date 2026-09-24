// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package decode

import (
	"strconv"

	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/pkg/codec"
)

const (
	PermAdmin          uint64 = 8
	PermKick           uint64 = 2
	PermBan            uint64 = 4
	PermModerate       uint64 = 1 << 40
	PermConnect        int64  = 1048576
	PermView           int64  = 1024
	PermSend           int64  = 2048
	PermReadHistory    int64  = 65536
	PermManageMessages int64  = 8192
)

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
	Member    struct {
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
	Channel   struct {
		Name string `json:"name"`
	} `json:"channel"`
	Member struct {
		User        UserRef  `json:"user"`
		Permissions string   `json:"permissions"`
		Nick        string   `json:"nick"`
		Roles       []string `json:"roles"`
	} `json:"member"`
}

func Decode[T any](raw []byte) (T, error) {
	var v T
	err := codec.Unmarshal(raw, &v)
	return v, err
}

func CanMod(permRaw string) bool {
	n, err := strconv.ParseUint(permRaw, 10, 64)
	if err != nil {
		return false
	}
	return n&(PermAdmin|PermKick|PermBan|PermModerate) != 0
}

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

func Mention(user UserRef) string { return "<@" + user.ID + ">" }

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

func FirstSub(opts []InteractionOption) InteractionOption {
	if len(opts) == 0 {
		return InteractionOption{}
	}
	return opts[0]
}

type OptionName string

func findOption(opts []InteractionOption, name OptionName) (InteractionOption, bool) {
	for _, o := range opts {
		if OptionName(o.Name) == name {
			return o, true
		}
	}
	return InteractionOption{}, false
}

func OptionString(sub InteractionOption, name OptionName) string {
	if o, ok := findOption(sub.Options, name); ok {
		return rawString(o.Value)
	}
	return ""
}

func OptionInt(sub InteractionOption, name OptionName) int {
	return OptionIntFrom(sub.Options, name)
}

func OptionIntFrom(opts []InteractionOption, name OptionName) int {
	o, ok := findOption(opts, name)
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(rawString(o.Value))
	return n
}

func OptionUser(opts []InteractionOption, name OptionName) string {
	for _, o := range opts {
		if OptionName(o.Name) == name {
			return rawString(o.Value)
		}
		if s := OptionUser(o.Options, name); s != "" {
			return s
		}
	}
	return ""
}

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

func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func HasAnyRole(roles []string, roleIDs []string) bool {
	for _, id := range roleIDs {
		if HasRole(roles, id) {
			return true
		}
	}
	return false
}
