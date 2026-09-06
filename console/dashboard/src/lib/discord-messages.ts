// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The refusal contract, as typed literal maps.
//
// Shared by both Discord pages rather than duplicated in each: the server list
// receives the OAuth slugs and the guild page receives the RPC codes, but a
// slug IS a code for every refusal that can happen in either place, and two
// copies of this table drift the first time outgress adds a code.
//
// The union types are what makes it safe: a code added here without copy in
// en.json fails to typecheck against the generated i18n key union.

// `locked` and `tickets_off` are console-local: they never cross the wire, and
// they exist so a refusal the dashboard itself decided is a translated
// sentence rather than a hardcoded English one leaking out of an action.
export const DISCORD_CODE_KEYS: Record<
  string,
  | 'discord.errBoundElsewhere'
  | 'discord.errNotBound'
  | 'discord.errUnavailable'
  | 'discord.errForbidden'
  | 'discord.errRateLimited'
  | 'discord.errInvalid'
  | 'discord.errConflict'
  | 'discord.errTimeout'
  | 'discord.errNotFound'
  | 'discord.errUnknown'
  | 'discord.errLocked'
  | 'discord.errTicketsOff'
> = {
  bound_elsewhere: 'discord.errBoundElsewhere',
  not_bound: 'discord.errNotBound',
  discord_unavailable: 'discord.errUnavailable',
  forbidden: 'discord.errForbidden',
  rate_limited: 'discord.errRateLimited',
  invalid: 'discord.errInvalid',
  conflict: 'discord.errConflict',
  timeout: 'discord.errTimeout',
  not_found: 'discord.errNotFound',
  unknown: 'discord.errUnknown',
  locked: 'discord.errLocked',
  tickets_off: 'discord.errTicketsOff'
};

export const DISCORD_SLUG_KEYS: Record<
  string,
  | (typeof DISCORD_CODE_KEYS)[string]
  | 'discord.errOauth'
  | 'discord.errUnconfigured'
  | 'discord.errSetup'
  | 'discord.errState'
  | 'discord.errNoGuilds'
> = {
  oauth: 'discord.errOauth',
  unconfigured: 'discord.errUnconfigured',
  setup: 'discord.errSetup',
  state: 'discord.errState',
  noguilds: 'discord.errNoGuilds',
  ...DISCORD_CODE_KEYS
};

// The bot pill, per state. Three states, not a boolean: a guild whose install
// predates a permission needs the streamer to act, and "offline" would send
// them to wait for a reconnect that already happened.
export const DISCORD_PILL_KEYS = {
  online: 'discord.statusOnline',
  offline: 'discord.statusOffline',
  reauth: 'discord.statusReauth'
} as const;

export const DISCORD_BADGE_KEYS = {
  mine: 'discord.pickMine',
  elsewhere: 'discord.pickElsewhere',
  addable: 'discord.pickAddable'
} as const;
