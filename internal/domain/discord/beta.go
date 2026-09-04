// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

// BetaPremiumOnly gates the whole Discord vertical to premium channels while
// it is in beta.
//
// This is the Go half of a flag that exists twice: the dashboard catalog marks
// DISCORD_MODULE `beta: true`, which is what shows the Beta chip, locks the
// tile and closes /discord and its form actions through the existing route
// guard. That half is presentation and write-blocking. This half is
// enforcement: a locked page still leaves a config row that a channel enabled
// earlier, or one written before the gate existed, and without a runtime check
// the bot would happily keep serving that guild.
//
// discord-beta-gate.test.ts reads this constant out of the source and fails if
// the two disagree, because the failure is otherwise silent in the direction
// that matters: the dashboard says premium-only and the bot serves everyone.
//
// Flipping this to false is how Discord leaves beta. Deleting the flag pair is
// the step after that -- see ModuleDef.beta's comment for why rows enabled
// during beta must keep working when it goes.
const BetaPremiumOnly = true

// PremiumGateOpen reports whether a broadcaster may use Discord.
//
// status is the projected account status and known says whether it could be
// read at all. An unreadable tier is treated as NOT premium: a Valkey blip
// must only ever close a paid feature, never open one. That is the opposite
// direction from IdentityFor, which leaves a guild's appearance alone when the
// tier is unknown, and deliberately so -- there, guessing wrong strips a
// paying streamer's badge; here, guessing wrong hands the beta to everyone.
func PremiumGateOpen(status string, known bool) bool {
	if !BetaPremiumOnly {
		return true
	}
	return known && IsPremium(status)
}
