// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export type ModuleTileHref = '/announcements' | '/community' | '/roles' | '/tickets';

export type ModuleTile = {
  id: ModuleTileId;
  flag: keyof DiscordConfig;
  on: boolean;
  href: ModuleTileHref;
  needs: (keyof DiscordConfig)[];
  ready: boolean;
};

type TileSpec = {
  id: ModuleTileId;
  flag: keyof DiscordConfig;
  href: ModuleTileHref;
  needs: (keyof DiscordConfig)[];
  defaultOn: boolean;
};

const TILES: readonly TileSpec[] = [
  { id: 'announcementsLive', flag: 'liveEnabled', href: '/announcements', needs: ['liveChannelId'], defaultOn: true },
  { id: 'announcementsClips', flag: 'clipsEnabled', href: '/announcements', needs: ['clipsChannelId'], defaultOn: true },
  { id: 'welcome', flag: 'welcomeEnabled', href: '/community', needs: ['welcomeChannelId'], defaultOn: true },
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

export function tilesNeedingSetup(tiles: readonly ModuleTile[]): ModuleTile[] {
  return tiles.filter((tile) => tile.on && !tile.ready);
}
