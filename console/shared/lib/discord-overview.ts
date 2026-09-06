// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// What the guild Overview shows: one row per Discord module, derived from the
// stored config alone.
//
// Pure and shared rather than computed in the page, for two reasons. The
// default of a flag is not "off" -- half of them are ON unless explicitly
// turned off (alertOn) and half are OFF unless explicitly turned on (alertOff),
// and getting that backwards renders a module as running when it is not. And a
// module being switched on is not the same as it WORKING: go-live posts with no
// channel picked are silently dropped by outgress, which from the dashboard
// looks exactly like a bug in the bot. `ready` is what lets the tile say so
// before the streamer goes looking.
import { alertOff, alertOn, type DiscordConfig } from './discord-config';

export type ModuleTileId =
  | 'announcementsLive'
  | 'announcementsClips'
  | 'welcome'
  | 'goodbye'
  | 'voiceHub'
  | 'logs'
  | 'levels'
  | 'linkGuard'
  | 'subscribers'
  | 'autoRole'
  | 'tickets';

/** The sub-page that owns a module's switch, relative to /discord/<guildId>. */
export type ModuleTileHref = '/announcements' | '/community' | '/roles' | '/tickets';

export type ModuleTile = {
  id: ModuleTileId;
  /** The DiscordConfig flag the tile's own switch writes. */
  flag: keyof DiscordConfig;
  on: boolean;
  href: ModuleTileHref;
  /** The picker fields the module cannot run without. */
  needs: (keyof DiscordConfig)[];
  /** Every `needs` field is set, so switching the module on actually does
   *  something. Computed regardless of `on` so a tile can be turned on and
   *  immediately report what it is still missing. */
  ready: boolean;
};

type TileSpec = {
  id: ModuleTileId;
  flag: keyof DiscordConfig;
  href: ModuleTileHref;
  needs: (keyof DiscordConfig)[];
  /** ON unless explicitly turned off. Mirrors alertOn/alertOff, which is the
   *  only place the real default of each flag is written down. */
  defaultOn: boolean;
};

// Order is the render order and it is part of the contract: the overview grid
// reads top-left to bottom-right as announcements, community, moderation,
// roles, tickets, which is the same order the sub-nav walks.
const TILES: readonly TileSpec[] = [
  { id: 'announcementsLive', flag: 'liveEnabled', href: '/announcements', needs: ['liveChannelId'], defaultOn: true },
  { id: 'announcementsClips', flag: 'clipsEnabled', href: '/announcements', needs: ['clipsChannelId'], defaultOn: true },
  { id: 'welcome', flag: 'welcomeEnabled', href: '/community', needs: ['welcomeChannelId'], defaultOn: true },
  // Goodbye posts into the welcome channel: there is no separate picker for it,
  // so it depends on the same field welcome does.
  { id: 'goodbye', flag: 'goodbyeEnabled', href: '/community', needs: ['welcomeChannelId'], defaultOn: false },
  { id: 'voiceHub', flag: 'voiceEnabled', href: '/community', needs: ['voiceHubId'], defaultOn: true },
  { id: 'logs', flag: 'logsEnabled', href: '/community', needs: ['logChannelId'], defaultOn: true },
  { id: 'levels', flag: 'levelsEnabled', href: '/community', needs: [], defaultOn: true },
  { id: 'linkGuard', flag: 'linkGuardEnabled', href: '/community', needs: [], defaultOn: false },
  { id: 'subscribers', flag: 'subscribersEnabled', href: '/community', needs: ['subsChannelId'], defaultOn: false },
  { id: 'autoRole', flag: 'autoRoleEnabled', href: '/roles', needs: ['memberRoleId'], defaultOn: true },
  { id: 'tickets', flag: 'ticketsEnabled', href: '/tickets', needs: ['ticketChannelId'], defaultOn: true }
];

function flagOn(config: DiscordConfig, spec: TileSpec): boolean {
  return spec.defaultOn ? alertOn(config[spec.flag]) : alertOff(config[spec.flag]);
}

function isReady(config: DiscordConfig, needs: (keyof DiscordConfig)[]): boolean {
  return needs.every((field) => config[field] !== '');
}

export function guildModuleTiles(config: DiscordConfig): ModuleTile[] {
  return TILES.map((spec) => ({
    id: spec.id,
    flag: spec.flag,
    on: flagOn(config, spec),
    href: spec.href,
    needs: [...spec.needs],
    ready: isReady(config, spec.needs)
  }));
}

/** Tiles that are on but cannot run yet. The overview's nudge counts these. */
export function tilesNeedingSetup(tiles: readonly ModuleTile[]): ModuleTile[] {
  return tiles.filter((tile) => tile.on && !tile.ready);
}
