// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package linkguard detects repeated links across channels or authors in one
// guild. Member sightings remain local: a guild's Twitch binding identifies
// its tenant but is not proof that the owner approved a link report. Passive
// sightings must never nominate other guilds' owners as global endorsers.
//
// Observe neither reads nor writes fleet promotion state, including legacy
// entries that may already have been poisoned. Local thresholds, exemptions,
// counting windows, and fail-open storage behavior remain unchanged. A future
// fleet policy needs an independently trusted approval entry point before it
// can affect guilds that never observed the link themselves.
package linkguard
