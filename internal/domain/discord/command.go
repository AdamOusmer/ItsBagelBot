// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

type Command struct {
	Type      string `json:"type"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Payload   []byte `json:"payload,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

const (
	LaneMod     = "discord.outgress.mod"
	LaneDefault = "discord.outgress.default"
)

const (
	TypeDeleteMessage = "delete_message"
	TypeBanMember     = "ban_member"
	TypeKickMember    = "kick_member"
	TypeTimeoutMember = "timeout_member"
	TypeStripRoles    = "strip_roles"
	TypeLockdown      = "lockdown"
	TypeUnlock        = "unlock"

	TypePostChat            = "post_chat"
	TypePostEmbed           = "post_embed"
	TypePostPanel           = "post_panel"
	TypeEditMessage         = "edit_message"
	TypeInteractionFollowup = "interaction_followup"
	TypeAddRole             = "add_role"
	TypeRemoveRole          = "remove_role"
	TypeSetGuildIdentity    = "set_guild_identity"
)

func ModType(commandType string) bool {
	switch commandType {
	case TypeDeleteMessage, TypeBanMember, TypeKickMember,
		TypeTimeoutMember, TypeStripRoles, TypeLockdown, TypeUnlock:
		return true
	default:
		return false
	}
}

func Lane(commandType string) string {
	if ModType(commandType) {
		return LaneMod
	}
	return LaneDefault
}
