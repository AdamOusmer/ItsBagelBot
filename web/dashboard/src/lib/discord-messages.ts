// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const DISCORD_CODE_KEYS: Record<
  string,
  | 'discord.errors.boundElsewhere'
  | 'discord.errors.notBound'
  | 'discord.errors.unavailable'
  | 'discord.errors.forbidden'
  | 'discord.errors.rateLimited'
  | 'discord.errors.invalid'
  | 'discord.errors.conflict'
  | 'discord.errors.timeout'
  | 'discord.errors.notFound'
  | 'discord.errors.unknown'
  | 'discord.errors.locked'
  | 'discord.errors.ticketsOff'
> = {
  bound_elsewhere: 'discord.errors.boundElsewhere',
  not_bound: 'discord.errors.notBound',
  discord_unavailable: 'discord.errors.unavailable',
  forbidden: 'discord.errors.forbidden',
  rate_limited: 'discord.errors.rateLimited',
  invalid: 'discord.errors.invalid',
  conflict: 'discord.errors.conflict',
  timeout: 'discord.errors.timeout',
  not_found: 'discord.errors.notFound',
  unknown: 'discord.errors.unknown',
  locked: 'discord.errors.locked',
  tickets_off: 'discord.errors.ticketsOff'
};

export const DISCORD_SLUG_KEYS: Record<
  string,
  | (typeof DISCORD_CODE_KEYS)[string]
  | 'discord.errors.oauth'
  | 'discord.errors.unconfigured'
  | 'discord.errors.setup'
  | 'discord.errors.state'
  | 'discord.errors.noGuilds'
> = {
  oauth: 'discord.errors.oauth',
  unconfigured: 'discord.errors.unconfigured',
  setup: 'discord.errors.setup',
  state: 'discord.errors.state',
  noguilds: 'discord.errors.noGuilds',
  ...DISCORD_CODE_KEYS
};

export const DISCORD_PILL_KEYS = {
  online: 'discord.status.online',
  offline: 'discord.status.offline',
  reauth: 'discord.status.reauth',
  unknown: 'discord.status.unknown'
} as const;

export const DISCORD_STATE_TAG = {
  online: { tone: 'live', mark: 'solid' },
  offline: { tone: 'danger', mark: 'solid' },
  reauth: { tone: 'alpha', mark: 'dash' },
  unknown: { tone: 'quiet', mark: 'hollow' }
} as const satisfies Record<
  keyof typeof DISCORD_PILL_KEYS,
  { tone: 'live' | 'danger' | 'alpha' | 'quiet'; mark: 'solid' | 'dash' | 'hollow' }
>;

export const DISCORD_BADGE_KEYS = {
  mine: 'discord.pick.mine',
  elsewhere: 'discord.pick.elsewhere',
  addable: 'discord.pick.addable'
} as const;
