// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The overview grid is the first thing a streamer sees for a server, so a tile
// that lies about a module is worse than no tile. Two failure modes are
// specifically pinned here: reading a default-ON flag as off (half of them are
// ON unless explicitly turned off), and calling a module healthy when the
// channel it posts to was never picked.

import { describe, expect, test } from 'bun:test';
import { blankDiscordConfig, type DiscordConfig } from './discord-config';
import { guildModuleTiles, tilesNeedingSetup, type ModuleTileId } from './discord-overview';

function configWith(patch: Partial<DiscordConfig>): DiscordConfig {
  return { ...blankDiscordConfig(), ...patch };
}

function tile(config: DiscordConfig, id: ModuleTileId) {
  const found = guildModuleTiles(config).find((t) => t.id === id);
  if (!found) throw new Error(`no tile ${id}`);
  return found;
}

describe('guildModuleTiles', () => {
  test('the order is stable and every module appears once', () => {
    const ids = guildModuleTiles(blankDiscordConfig()).map((t) => t.id);
    expect(ids).toEqual([
      'announcementsLive',
      'announcementsClips',
      'welcome',
      'goodbye',
      'voiceHub',
      'logs',
      'levels',
      'linkGuard',
      'subscribers',
      'autoRole',
      'tickets'
    ]);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test('a blank config reads each flag at its real default, not at off', () => {
    const on = guildModuleTiles(blankDiscordConfig())
      .filter((t) => t.on)
      .map((t) => t.id);
    // Default ON: go-live, clips, welcome, voice hub, logs, levels, autorole,
    // tickets. Default OFF: goodbye, link guard, subscriber tier.
    expect(on).toEqual([
      'announcementsLive',
      'announcementsClips',
      'welcome',
      'voiceHub',
      'logs',
      'levels',
      'autoRole',
      'tickets'
    ]);
  });

  test('an explicit off turns a default-on module off, and back on', () => {
    expect(tile(configWith({ liveEnabled: 'off' }), 'announcementsLive').on).toBe(false);
    expect(tile(configWith({ liveEnabled: 'on' }), 'announcementsLive').on).toBe(true);
  });

  test('an explicit on turns a default-off module on', () => {
    expect(tile(blankDiscordConfig(), 'linkGuard').on).toBe(false);
    expect(tile(configWith({ linkGuardEnabled: 'on' }), 'linkGuard').on).toBe(true);
  });

  test('ready follows the picker fields, independently of the switch', () => {
    const blank = tile(blankDiscordConfig(), 'announcementsLive');
    expect(blank.needs).toEqual(['liveChannelId']);
    expect(blank.ready).toBe(false);

    const picked = tile(configWith({ liveChannelId: '123456789012345678' }), 'announcementsLive');
    expect(picked.ready).toBe(true);

    // Off but configured: ready must not be conflated with on.
    const offButPicked = tile(
      configWith({ liveEnabled: 'off', liveChannelId: '123456789012345678' }),
      'announcementsLive'
    );
    expect(offButPicked.on).toBe(false);
    expect(offButPicked.ready).toBe(true);
  });

  test('a module with nothing to pick is always ready', () => {
    expect(tile(blankDiscordConfig(), 'levels').needs).toEqual([]);
    expect(tile(blankDiscordConfig(), 'levels').ready).toBe(true);
  });

  test('goodbye rides the welcome channel, because it has no picker of its own', () => {
    expect(tile(blankDiscordConfig(), 'goodbye').needs).toEqual(['welcomeChannelId']);
    expect(tile(configWith({ welcomeChannelId: '1' }), 'goodbye').ready).toBe(true);
  });

  test('each tile links to the sub-page that owns its switch', () => {
    const byId = Object.fromEntries(guildModuleTiles(blankDiscordConfig()).map((t) => [t.id, t.href]));
    expect(byId).toEqual({
      announcementsLive: '/announcements',
      announcementsClips: '/announcements',
      welcome: '/community',
      goodbye: '/community',
      voiceHub: '/community',
      logs: '/community',
      levels: '/community',
      linkGuard: '/community',
      subscribers: '/community',
      autoRole: '/roles',
      tickets: '/tickets'
    });
  });
});

describe('tilesNeedingSetup', () => {
  test('only counts modules that are on and missing a pick', () => {
    const tiles = guildModuleTiles(blankDiscordConfig());
    const ids = tilesNeedingSetup(tiles).map((t) => t.id);
    // Goodbye, link guard and the subscriber tier are off by default, so they
    // are not nagged about; levels needs nothing at all.
    expect(ids).toEqual([
      'announcementsLive',
      'announcementsClips',
      'welcome',
      'voiceHub',
      'logs',
      'autoRole',
      'tickets'
    ]);
  });

  test('a fully picked server nags about nothing', () => {
    const config = configWith({
      liveChannelId: '1',
      clipsChannelId: '2',
      welcomeChannelId: '3',
      voiceHubId: '4',
      logChannelId: '5',
      memberRoleId: '6',
      ticketChannelId: '7'
    });
    expect(tilesNeedingSetup(guildModuleTiles(config))).toEqual([]);
  });
});
