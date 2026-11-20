// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package invalidate

type Scope = string

const LoyaltyFreezeMarker = "freeze"

const (
	ScopeStatus          Scope = "status"
	ScopeGrant           Scope = "grant"
	ScopeLocale          Scope = "locale"
	ScopeCursor          Scope = "cursor"
	ScopeUser            Scope = "user"
	ScopeUserProjection  Scope = "user-projection"
	ScopeRename          Scope = "rename"
	ScopeDelegation      Scope = "delegation"
	ScopeNotifications   Scope = "notifications"
	ScopeModules         Scope = "modules"
	ScopeCommands        Scope = "commands"
	ScopeFetches         Scope = "fetches"
	ScopeAutomations     Scope = "automations"
	ScopeLive            Scope = "live"
	ScopeTokenWarm       Scope = "token-warm"
	ScopeOutgress        Scope = "outgress"
	ScopeOutgressPause   Scope = "outgress-pause"
	ScopeLoyalty         Scope = "loyalty"
	ScopeCommandsPage    Scope = "commands_page"
	ScopeLeaderboardPage Scope = "leaderboard_page"
	ScopeSongQueuePage   Scope = "songqueue_page"
	ScopeSpotify         Scope = "spotify"
	ScopeLoyaltyCounters Scope = "loyalty-counters"
)
