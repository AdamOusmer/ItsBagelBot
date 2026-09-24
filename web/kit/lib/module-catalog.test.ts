// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MOD, MODULE_CATALOG, catalogIndexable, moduleDef, moduleDelegateSections } from './types';

describe('module catalog', () => {
  test('has unique ids', () => {
    const ids = MODULE_CATALOG.map((def) => def.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test('every MOD entry names a real catalog module', () => {
    for (const [key, value] of Object.entries(MOD)) {
      expect(key).toBe(value);
      expect(moduleDef(value)).toBeDefined();
    }
  });

  test('every catalog module has a MOD key', () => {
    const catalogIds = MODULE_CATALOG.map((def) => def.id).sort();
    const modValues = new Set(Object.values(MOD));
    const missing = catalogIds.filter((id) => !modValues.has(id));
    expect(missing).toEqual([]);
  });

  test('stream management is a Channel tile on /commands with no master switch', () => {
    const def = moduleDef('stream');
    expect(def).toBeDefined();
    expect(def?.href).toBe('/commands');
    expect(def?.toggleable).toBe(false);
    expect(def?.defaultEnabled).toBe(true);
    expect(def?.category).toBe('Channel');
    expect(moduleDelegateSections(def!)).toEqual(['commands']);
  });

  test('stream management ships the Nightbot command set at lead_mod', () => {
    const commands = moduleDef('stream')?.commands;
    expect(commands?.map((c) => c.trigger)).toEqual([
      '!title',
      '!game',
      '!tags',
      '!commercial',
      '!marker',
      '!cmd'
    ]);
    const title = commands?.find((c) => c.trigger === '!title');
    expect(title?.perm).toBe('lead_mod');
    expect(title?.aliases).toEqual(['!settitle']);
  });

  test('folds counters into Modules delegation without an enable switch', () => {
    const counters = moduleDef('counters');
    expect(counters).toBeDefined();
    expect(counters?.href).toBe('/counters');
    expect(counters?.toggleable).toBe(false);
    expect(moduleDelegateSections(counters!)).toEqual(['modules']);
  });

  test('emoteplay is a toggle-only opt-in module keyed by its sesame name', () => {
    const def = moduleDef('emoteplay');
    expect(def).toBeDefined();
    expect(def?.href).toBeUndefined();
    expect(def?.hidden).toBeFalsy();
    expect(def?.toggleable).not.toBe(false);
    expect(def?.defaultEnabled).toBe(false);
    expect(def?.replies).toHaveLength(0);
  });

  test('personality is a default-on Chat toggle with nothing to configure', () => {
    const def = moduleDef('personality');
    expect(def).toBeDefined();
    expect(def?.category).toBe('Chat');
    expect(def?.defaultEnabled).toBe(true);
    expect(def?.toggleable).not.toBe(false);
    expect(def?.href).toBeUndefined();
    expect(def?.hidden).toBeFalsy();
    expect(def?.replies).toHaveLength(0);
    expect(def?.settings).toBeUndefined();
  });

  test('personality owns !bagels and !bagelboard with their aliases', () => {
    const commands = moduleDef('personality')?.commands;
    expect(commands?.map((c) => c.trigger)).toEqual(['!bagels', '!bagelboard']);
    expect(commands?.[0].aliases).toEqual(['!fed', '!bagelcount']);
    expect(commands?.[1].aliases).toEqual(['!feedboard', '!bagellb']);
    expect(commands?.every((c) => c.perm === undefined)).toBe(true);
  });

  test('CODM is a generic Stats profile module with the shared account settings', () => {
    const def = moduleDef('codm');
    expect(def).toBeDefined();
    if (!def) throw new Error('CODM module missing');
    expect(def.label).toBe('CODM Profile');
    expect(def.category).toBe('Stats');
    expect(def.defaultEnabled).toBe(false);
    expect(def.replies).toHaveLength(1);

    const profile = def.replies[0];
    expect(profile).toMatchObject({
      key: 'profile',
      label: '!codm',
      command: 'codm',
      event: '!codm [UID/exact nickname]',
      enableKey: 'profileEnabled',
      messageKey: 'profileMessage',
      defaultMessage: '{player} · level {level} · MP {rank} · {rating} rating · {country}',
      previewArgs: 'iFerg',
      tokens: [
        { name: 'player', sample: 'iFerg' },
        { name: 'level', sample: '414' },
        { name: 'rank', sample: 'Master I' },
        { name: 'rankclass', sample: '21' },
        { name: 'rating', sample: '4590' },
        { name: 'country', sample: 'US' },
        { name: 'shortid', sample: 'IFERG' }
      ]
    });
    expect(def.settings![0]).toMatchObject({
      key: 'account',
      help: 'Default profile for the command. If blank, enter a CODM UID or exact nickname after !codm.'
    });
    expect(profile.tokens?.find((tk) => tk.name === 'player')?.sample).toBe(profile.previewArgs);
    expect(def.settings!.map((field) => field.key)).toEqual(['account', 'linkedOnly']);
  });

  test('govee shares Gear with Song Requests and Discord', () => {
    expect(moduleDef('govee')?.category).toBe('Gear');
    expect(moduleDef('songqueue')?.category).toBe('Gear');
    expect(moduleDef('discord')?.category).toBe('Gear');
    expect(moduleDef('discord')?.href).toBe('/discord');
  });

  test('a sectioned module is kept out of the modules grid', () => {
    const discord = moduleDef('discord');
    expect(discord?.section).toBe(true);
    expect(discord && catalogIndexable(discord)).toBe(false);
    expect(discord?.hidden).toBeUndefined();
  });

  test('every other listed module still indexes', () => {
    const listed = MODULE_CATALOG.filter((d) => !d.hidden && !d.parent && !d.section);
    expect(listed.length).toBeGreaterThan(0);
    expect(listed.every(catalogIndexable)).toBe(true);
  });

  test('discord delegates on its own grant, not modules', () => {
    const discord = moduleDef('discord');
    expect(discord).toBeDefined();
    expect(moduleDelegateSections(discord!)).toEqual(['discord']);
  });

  test('AutoMod stays a visible Moderation row', () => {
    const def = moduleDef('automod');
    expect(def?.hidden).toBeFalsy();
    expect(def?.category).toBe('Moderation');
  });

  test('songqueue is a bespoke href module listing !sr, !remove, !skip, !clear, !srlist and !current', () => {
    const def = moduleDef('songqueue');
    expect(def).toBeDefined();
    expect(def?.href).toBe('/songqueue');
    expect(def?.commands?.map((c) => c.trigger)).toEqual([
      '!sr',
      '!remove',
      '!skip',
      '!clear',
      '!srlist',
      '!current'
    ]);
  });

  test('songqueue opens for modules and channel-points delegates', () => {
    const def = moduleDef('songqueue');
    expect(def).toBeDefined();
    expect(moduleDelegateSections(def!)).toEqual(['modules', 'channelpoints']);
  });

  test('gamble and duels nest under loyalty with no second currency name', () => {
    for (const id of ['gamble', 'duel'] as const) {
      const def = moduleDef(id);
      expect(def?.parent).toBe('loyalty');
      expect(def?.href).toBeUndefined();
      expect(def?.settings?.some((s) => s.key === 'pointsName')).toBe(false);
    }
    expect(moduleDef('loyalty')?.href).toBe('/loyalty');
  });
});
