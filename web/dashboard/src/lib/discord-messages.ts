// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export const DISCORD_PILL_KEYS = {
  online: 'discord.statusOnline',
  offline: 'discord.statusOffline',
  reauth: 'discord.statusReauth',
  unknown: 'discord.statusUnknown'
} as const;

export const DISCORD_STATE_TAG = {
  online: { tone: 'live', mark: 'solid' },
  offline: { tone: 'error', mark: 'solid' },
  reauth: { tone: 'alpha', mark: 'dash' },
  unknown: { tone: 'quiet', mark: 'hollow' }
} as const satisfies Record<
  keyof typeof DISCORD_PILL_KEYS,
  { tone: 'live' | 'error' | 'alpha' | 'quiet'; mark: 'solid' | 'dash' | 'hollow' }
>;

export const DISCORD_BADGE_KEYS = {
  mine: 'discord.pickMine',
  elsewhere: 'discord.pickElsewhere',
  addable: 'discord.pickAddable'
} as const;
