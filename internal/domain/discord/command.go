// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

// Command is what app/discord/engine emits and app/discord/outgress executes:
// one Discord REST call, described rather than performed. It is the Discord
// half of outgress.Message, and deliberately the same shape, so the two
// verticals stay readable side by side.
//
// The engine never calls Discord. Only outgress holds the REST client and the
// one shared token bucket, because Discord's global limit is per BOT TOKEN:
// the moment two services call with the same token, neither can see the other's
// spend and both discover the limit by being 429'd.
type Command struct {
	// Type selects the outgress action (see the Type* constants).
	Type string `json:"type"`
	// GuildID and ChannelID target the call.
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id,omitempty"`
	// UserID is the subject of a member-scoped action (ban, kick, timeout,
	// role change), not the actor -- the actor is always the bot.
	UserID string `json:"user_id,omitempty"`
	// Payload is the type-specific body, marshalled by the engine.
	Payload []byte `json:"payload,omitempty"`
	// Reason rides Discord's X-Audit-Log-Reason header where the endpoint
	// supports it, so a moderator reading the audit log sees WHY the bot acted
	// rather than an unexplained bot ban. Automod actions must always set it.
	Reason string `json:"reason,omitempty"`
}

// Outgress lanes. Two, and the split is by urgency, not by customer tier as on
// the Twitch side.
//
// Discord's global budget is roughly 50 requests/second for the whole bot
// token, shared across every guild. A mass-join raid produces two demands on
// that budget at once: hundreds of welcome embeds, and the handful of calls
// that actually stop the raid. On one queue the moderation calls land behind
// the cosmetic ones and the lockdown arrives after the damage -- the failure is
// not dropped work, it is work that completes far too late to matter.
//
// So moderation preempts. Outgress drains LaneMod to empty before it touches
// LaneDefault, and a welcome that waits out a raid is the correct outcome.
const (
	// LaneMod carries anything that stops an attack or enforces a rule: bans,
	// kicks, timeouts, message deletes, role strips, channel lockdowns,
	// verification-level changes. Latency-critical.
	LaneMod = "discord.outgress.mod"
	// LaneDefault carries everything else: welcomes, go-live and clip embeds,
	// rank cards, ticket panels, log lines. Latency-tolerant by construction.
	LaneDefault = "discord.outgress.default"
)

// Command types outgress dispatches on. Moderation types are listed first
// because they are the ones with a deadline.
const (
	// TypeDeleteMessage removes one message. The automod's primary action:
	// cheaper than a ban, reversible in effect (the poster can be warned), and
	// the only action that scales to a spam wave without exhausting the budget.
	TypeDeleteMessage = "delete_message"
	// TypeBanMember and TypeKickMember remove an account from the guild.
	TypeBanMember  = "ban_member"
	TypeKickMember = "kick_member"
	// TypeTimeoutMember mutes without removing, for humans who misbehave
	// rather than accounts that are hostile.
	TypeTimeoutMember = "timeout_member"
	// TypeStripRoles removes every removable role from a member. The nuke
	// response: a compromised admin is disarmed by taking their permissions,
	// which works even while their session is still live and deleting things.
	TypeStripRoles = "strip_roles"
	// TypeLockdown raises the guild's verification level (and pauses invites
	// where available). One call that blunts an entire raid, versus one ban per
	// attacker against a 50/s budget -- always prefer it at scale.
	TypeLockdown = "lockdown"

	// TypePostChat, TypePostEmbed and TypePostPanel write to a channel.
	TypePostChat  = "post_chat"
	TypePostEmbed = "post_embed"
	TypePostPanel = "post_panel"
	// TypeEditMessage edits an existing message, e.g. flipping the go-live
	// embed to "stream ended" rather than posting a second message.
	TypeEditMessage = "edit_message"
	// TypeInteractionFollowup completes an interaction ingress already
	// deferred. It must not be dropped silently: a deferred interaction whose
	// followup never lands leaves the user staring at a permanent "thinking..."
	// spinner, which reads as more broken than an error message.
	TypeInteractionFollowup = "interaction_followup"
	// TypeAddRole and TypeRemoveRole manage the @Live role and autorole.
	TypeAddRole    = "add_role"
	TypeRemoveRole = "remove_role"
)

// ModTypes reports whether a command belongs on LaneMod. Kept as one function
// rather than a field the producer sets, so a new moderation type cannot be
// introduced onto the slow lane by an author who forgot to set the flag.
func ModType(commandType string) bool {
	switch commandType {
	case TypeDeleteMessage, TypeBanMember, TypeKickMember,
		TypeTimeoutMember, TypeStripRoles, TypeLockdown:
		return true
	default:
		return false
	}
}

// Lane returns the subject a command must be published on.
func Lane(commandType string) string {
	if ModType(commandType) {
		return LaneMod
	}
	return LaneDefault
}
