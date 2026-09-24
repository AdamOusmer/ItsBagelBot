// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

type ButtonSpec struct {
	Style    int    `json:"style,omitempty"`
	Label    string `json:"label"`
	CustomID string `json:"custom_id"`
}

type EmbedPayload struct {
	Content string       `json:"content,omitempty"`
	Embed   Embed        `json:"embed"`
	Buttons []ButtonSpec `json:"buttons,omitempty"`
}

type ChatPayload struct {
	Content string `json:"content"`
}

type EditPayload struct {
	MessageID string  `json:"message_id"`
	Content   string  `json:"content"`
	Embeds    []Embed `json:"embeds,omitempty"`
}

type DeletePayload struct {
	MessageID string `json:"message_id"`
}

type TimeoutPayload struct {
	UntilISO string `json:"until_iso,omitempty"`
}

type RolePayload struct {
	RoleID string `json:"role_id"`
}

type FollowupPayload struct {
	InteractionToken string       `json:"interaction_token"`
	Content          string       `json:"content,omitempty"`
	Embed            *Embed       `json:"embed,omitempty"`
	Buttons          []ButtonSpec `json:"buttons,omitempty"`
	Ephemeral        bool         `json:"ephemeral,omitempty"`
}

type IdentityPayload struct {
	Identity GuildIdentity `json:"identity"`
}

type LockdownPayload struct {
	EveryoneRoleID string   `json:"everyone_role_id,omitempty"`
	CategoryIDs    []string `json:"category_ids,omitempty"`
}
