// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "strings"

// PremiumNick is the per-guild nickname the bot wears in a premium
// streamer's server. Discord caps a nickname at 32 characters; this is 21,
// so it is never truncated.
//
// Per-GUILD, not global. A bot has exactly one global username and avatar
// across every server it is in, so "rename the bot for premium users" is only
// possible because Modify Current Member
// (PATCH /guilds/{id}/members/@me) takes nick, avatar, banner and bio scoped
// to one guild. Changing the global identity instead would rename the bot in
// every free server too, and Discord rate-limits global username changes
// severely on top of that.
const PremiumNick = "ItsBagelBot - Premium"

// Premium statuses. VIP and paid are deliberately the same identity: the
// product treats them as one tier, and giving VIP a third look would mean a
// third asset to keep in sync for no stated benefit.
const (
	StatusPaid = "paid"
	StatusVIP  = "vip"
)

// IsPremium reports whether a projected user status earns the premium
// identity. Case and surrounding whitespace are normalized because this value
// arrives from two places -- the users projection and the data.users.changed
// event -- and only one of them is guaranteed to have been written by code in
// this repo.
func IsPremium(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusPaid, StatusVIP:
		return true
	default:
		return false
	}
}

// GuildIdentity is the bot's appearance in one guild. It is a two-state value,
// not a free-form one: premium, or the default.
//
// The default state clears rather than overwrites. Discord treats a null nick
// and a null avatar on Modify Current Member as "remove the guild override and
// fall back to my global identity", which is exactly what a downgrade should
// do -- writing the global name back as a guild override would leave a stale
// nickname pinned the day the global name changes.
type GuildIdentity struct {
	// Premium selects the state. The avatar bytes are NOT carried here: the
	// image is ~86 KB base64 and this value travels on a work-queue lane, so
	// shipping it per command would put the same picture on the wire for every
	// guild on every reconnect. Outgress owns the embedded asset and reads
	// this flag to decide whether to send it.
	Premium bool `json:"premium"`
}

// Nick returns the nickname for this identity, and whether it should be set at
// all. A false second return means "clear the override", which is not the same
// as setting an empty string.
func (g GuildIdentity) Nick() (string, bool) {
	if !g.Premium {
		return "", false
	}
	return PremiumNick, true
}

// IdentityFor maps a projected user status onto the identity their guild
// should show.
func IdentityFor(status string) GuildIdentity {
	return GuildIdentity{Premium: IsPremium(status)}
}

// Fingerprint is the value cached per guild so a repeated apply can be
// skipped. GUILD_CREATE fires for every guild on every gateway reconnect, so
// without this a reconnect would re-upload the premium avatar once per guild
// and burn the shared per-token rate budget on work that changes nothing.
func (g GuildIdentity) Fingerprint() string {
	if g.Premium {
		return "premium"
	}
	return "default"
}
