// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package linkguard detects one specific pattern that Discord's own AutoMod
// does not catch: a compromised account (or a small batch of accounts
// compromised together) posting the same NSFW/scam invite link across many
// channels of one guild, and that same link then reappearing across many
// unrelated guilds in the fleet. The signal this package acts on is
// REPETITION -- across channels, then across guilds -- never the content of
// the link. It never fetches, resolves, or classifies a URL; a link is
// "known bad" here purely because the fleet has independently seen it spam
// itself in enough different places, not because anyone judged its content.
//
// # Why distinct channels, not message count
//
// One account posting a link 50 times in one channel is message flooding: a
// different problem with a different fix (a per-channel rate limit), and
// Discord's native flood/AutoMod tooling already covers it reasonably well.
// Counting messages here would conflate the two and either miss the
// channel-spray pattern (drowned out by a single loud channel) or fire on a
// user who is simply chatty in one place. Counting DISTINCT CHANNELS keys
// the detector to the actual attack shape: a hijacked session working
// through a channel list. See Observe and ChannelThreshold.
//
// # Why cross-owner corroboration gates fleet promotion
//
// Once a link is promoted "known bad" fleet-wide, every guild the fleet
// serves acts on it -- including guilds that never saw it themselves. That
// is exactly what makes fleet promotion powerful, and exactly what makes it
// an abuse vector. The obvious guard is "require independent trips from
// more than one guild" -- but a Discord GUILD is free to create and
// self-service to add the bot to, so that guard alone is not a real bar:
// one operator can stand up a second (or third, or tenth) guild in
// seconds, trip each one's own local threshold by spamming a rival
// community's invite across their own channels, and clear any guild-COUNT
// bar at essentially zero cost. Promotion instead requires independent
// trips from FleetOwnerThreshold distinct Twitch broadcaster OWNERS (the
// discord:guild:{id} binding each guild gets during setup -- see
// Sighting.OwnerID), because enrolling a second Twitch channel with us is
// not free the way creating a second Discord server is (see the
// constant's doc for the full reasoning). A guild with no owner binding on
// record (setup never completed) cannot contribute to corroboration at
// all -- see corroborate -- so skipping setup is not a way around this
// either. A single owner, however many guilds they puppet, can never
// promote a link on their own -- they can only get that link actioned
// inside guilds they actually control. See fleetPromote and the package's
// tests for the exact "1 owner trips, nothing promotes; 2 distinct owners
// trip, the link promotes; a 3rd guild (with a 3rd, uninvolved owner) that
// never saw it now gets a hit" sequence this buys, and the regression test
// for "2 guilds, SAME owner, does not promote".
//
// # Why normalization happens before any counting
//
// discord.gg/X, discord.com/invite/X, discordapp.com/invite/X and
// DISCORD.GG/X (any case, any host alias, with or without a scheme, with or
// without Discord's own "<...>" embed-suppression brackets, with or without
// trailing sentence punctuation) are the same invite. If counting used the
// raw string, an attacker evades every threshold in this package for free
// by rotating the host alias or the letter case on each post -- the
// distinct-channel and distinct-author sets would never see the same key
// twice. Normalization happens once, before the value is ever used as a
// Valkey key, so every downstream count operates on canonical identity.
// See NormalizeLink for exactly what folds together and why.
//
// # Scope
//
// No Discord API calls, no NATS, no HTTP: persistence is Valkey only, and
// every decision is a pure function of a Sighting plus what is already in
// Valkey. That is what keeps this package trivially unit-testable and
// reusable outside a live gateway connection. The caller (the engine that
// owns Discord API access) resolves anything Discord-shaped -- whether a
// link is the guild's own invite, whether the author is a moderator,
// whether the guild's allow-list matches -- and hands the answer in on the
// Sighting; see Sighting's fields. Observe returns a Verdict, which is
// advisory data describing what was observed and which threshold (if any)
// tripped -- never an instruction like "ban this user". Deciding what
// enforcement action to take, and taking it, is the caller's job.
//
// # Valkey key shapes (all under the "discord:linkguard:" prefix)
//
//	discord:linkguard:channels:{guildID}:{normalizedLink}  SET of channel IDs   TTL Window
//	discord:linkguard:authors:{guildID}:{normalizedLink}   SET of user IDs      TTL Window
//	discord:linkguard:trips:{normalizedLink}               SET of owner IDs     TTL CorroborationWindow
//	discord:linkguard:fleet:{normalizedLink}                HASH (see fleetPromote)  TTL FleetTTL
//
// # Non-invite links
//
// This machinery generalizes to any URL, not just Discord invites, and
// NormalizeLink documents how a non-invite link is normalized (host+path,
// tagged with a "url:" prefix rather than "invite:"). That generalization
// is deliberate but explicitly less validated than the invite path: the
// invite normalization rules above are exact and enumerated, while the
// generic URL path is a best-effort fallback. Do not assume a non-invite
// link is folded as aggressively (tracking-parameter stripping, mirrors,
// shorteners) as an invite is.
package linkguard
