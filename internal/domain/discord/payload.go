// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

// ButtonSpec is one message-component button, carried inside a Command
// Payload rather than internal/discordapi.Button directly: this package is
// imported by both app/discord/engine (which builds Commands) and
// app/discord/outgress (which executes them), and neither needs the other's
// full REST client surface just to agree on this shape.
type ButtonSpec struct {
	Style    int    `json:"style,omitempty"`
	Label    string `json:"label"`
	CustomID string `json:"custom_id"`
}

// EmbedPayload is Command.Payload's shape for TypePostEmbed and
// TypePostPanel (Buttons empty for a plain embed post).
type EmbedPayload struct {
	Content string       `json:"content,omitempty"`
	Embed   Embed        `json:"embed"`
	Buttons []ButtonSpec `json:"buttons,omitempty"`
}

// ChatPayload is Command.Payload's shape for TypePostChat.
type ChatPayload struct {
	Content string `json:"content"`
}

// EditPayload is Command.Payload's shape for TypeEditMessage. MessageID
// names the message within Command.ChannelID; Embeds nil leaves the
// existing embeds alone, non-nil (including empty) replaces them.
type EditPayload struct {
	MessageID string  `json:"message_id"`
	Content   string  `json:"content"`
	Embeds    []Embed `json:"embeds,omitempty"`
}

// DeletePayload is Command.Payload's shape for TypeDeleteMessage.
type DeletePayload struct {
	MessageID string `json:"message_id"`
}

// TimeoutPayload is Command.Payload's shape for TypeTimeoutMember. Empty
// UntilISO clears an existing timeout.
type TimeoutPayload struct {
	UntilISO string `json:"until_iso,omitempty"`
}

// RolePayload is Command.Payload's shape for TypeAddRole and TypeRemoveRole.
type RolePayload struct {
	RoleID string `json:"role_id"`
}

// FollowupPayload is Command.Payload's shape for TypeInteractionFollowup.
// InteractionToken is the deferred interaction's webhook token; outgress
// pairs it with its own application id (learned once at boot, see
// app/discord/outgress) to address the webhook. See internal/discordapi's
// Followup for the REST call this becomes.
type FollowupPayload struct {
	InteractionToken string       `json:"interaction_token"`
	Content          string       `json:"content,omitempty"`
	Embed            *Embed       `json:"embed,omitempty"`
	Buttons          []ButtonSpec `json:"buttons,omitempty"`
	Ephemeral        bool         `json:"ephemeral,omitempty"`
}

// IdentityPayload carries the bot's per-guild appearance for a
// TypeSetGuildIdentity Command. It holds a flag rather than image bytes:
// outgress embeds the avatar and picks it from this flag, so the same ~86 KB
// picture never rides the lane once per guild. See GuildIdentity.
type IdentityPayload struct {
	Identity GuildIdentity `json:"identity"`
}

// LockdownPayload is Command.Payload's shape for TypeLockdown.
//
// The category ids ride the command rather than being read from Config by
// outgress because outgress never reads the module blob (see the setup
// package's doc): engine decides WHAT to lock, outgress only performs it.
// An empty CategoryIDs raises the guild's verification level and nothing
// else, which is still the bulk of a lockdown's effect.
type LockdownPayload struct {
	// EveryoneRoleID is the role the SEND denial is written onto. Empty
	// falls back to the guild id, which is what Discord's @everyone role id
	// always equals.
	EveryoneRoleID string `json:"everyone_role_id,omitempty"`
	// CategoryIDs are the categories whose text channels get muted.
	CategoryIDs []string `json:"category_ids,omitempty"`
}
