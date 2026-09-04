// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

// Event is what app/discord/ingress publishes for every gateway event it
// receives, and the only thing app/discord/engine consumes. It mirrors the
// Twitch shape (ingress -> engine -> outgress) rather than inventing a second
// one, so the reasoning that already applies to twitch.ingress.event.* applies
// here too.
//
// Ingress NEVER acts on an event. It receives, wraps, publishes. That is not
// tidiness: the gateway is a singleton (one Identify session per bot token),
// and the previous design called Discord REST inline on the receive path, so a
// throttled call blocked the one process able to read from the socket. A
// mass-join raid -- the exact case the automod exists for -- would stall event
// reception while the flood was still arriving, and Discord does not redeliver
// what you failed to read. Publishing is the backpressure boundary.
//
// The one exception is INTERACTION_CREATE. Discord gives 3 seconds to
// acknowledge an interaction, which is not a budget to spend on a broker
// round-trip, so ingress ACKs (defers) inline and publishes the work for the
// engine to finish through the interaction webhook.
type Event struct {
	// Type is the gateway event name verbatim (GUILD_MEMBER_ADD,
	// MESSAGE_CREATE, ...). Kept as Discord's own string rather than remapped
	// to an internal enum so an unrecognized event is loggable as itself.
	Type string `json:"type"`
	// GuildID scopes every downstream lookup: the module blob, the automod
	// counters, and the guild->broadcaster binding. A DM carries none, and the
	// engine drops those -- this bot has no DM surface.
	GuildID string `json:"guild_id"`
	// ChannelID and UserID are lifted out of the payload because the engine
	// routes and rate-keys on them without needing to decode the body.
	ChannelID string `json:"channel_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	// Raw is the untouched gateway payload. Modules decode the slice of it
	// they care about, the same way sesame modules decode c.Env.Event.
	Raw []byte `json:"raw"`
	// ReceivedAtUnixMs stamps arrival at ingress, not engine. Raid detection
	// counts events per window, and measuring that window from the engine's
	// clock would make a consumer backlog look like a slower raid -- exactly
	// backwards, since a backlog is evidence of a faster one.
	ReceivedAtUnixMs int64 `json:"received_at_unix_ms"`
}

// Ingress subjects. Enumerated rather than a discord.ingress.event.> wildcard,
// matching TWITCH_INGRESS and for the same reason: two streams may not claim
// overlapping subjects, so a wildcard cannot survive a later lane split, and
// the failure mode when it does is silent (a subject captured by no stream
// publishes into nothing). Adding an event class means adding it here AND to
// DiscordIngressStream.Subjects.
const (
	// SubjectEventMessage carries MESSAGE_CREATE / UPDATE / DELETE. This is the
	// automod's firehose and by far the highest volume.
	SubjectEventMessage = "discord.ingress.event.message"
	// SubjectEventMember carries GUILD_MEMBER_ADD / REMOVE: welcomes, goodbyes,
	// autorole, and the join-rate signal a raid is measured on.
	SubjectEventMember = "discord.ingress.event.member"
	// SubjectEventVoice carries VOICE_STATE_UPDATE for join-to-create.
	SubjectEventVoice = "discord.ingress.event.voice"
	// SubjectEventInteraction carries the deferred half of a slash command or
	// button press. Ingress has already ACKed by the time this is published;
	// the engine's reply goes out through the interaction webhook, which is why
	// a late one is recoverable and a late ACK is not.
	SubjectEventInteraction = "discord.ingress.event.interaction"
	// SubjectEventAudit carries GUILD_AUDIT_LOG_ENTRY_CREATE, the only way to
	// see a nuke in progress: mass channel deletes or mass bans by an account
	// with permissions, which look like ordinary authorized actions on every
	// other event stream.
	SubjectEventAudit = "discord.ingress.event.audit"
	// SubjectEventGuild carries GUILD_CREATE and friends: the bot's own view of
	// which guilds it is in, and the layout it can act on.
	SubjectEventGuild = "discord.ingress.event.guild"
)
