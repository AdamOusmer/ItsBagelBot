// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MOD, MODULE_CATALOG, catalogIndexable, moduleDef, moduleDelegateSections } from './types';

describe('module catalog', () => {
  test('has unique ids', () => {
    const ids = MODULE_CATALOG.map((def) => def.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  // MOD is a hand-written id map sitting next to 22 independently-declared
  // module ids (each catalog/<name>.ts owns its own `id: '<name>'`). A typo
  // in either place used to compile fine and just silently miss the module
  // blob at runtime. These two directions catch a renamed id on one side and
  // a new module that never got a MOD key on the other.
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
    expect(def?.icon).toBe('broadcast');
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

  // The emoteplay tile must stay a plain opt-in toggle: no bespoke page, no
  // delegation grant of its own, and an id matching the sesame module name the
  // engine gates on (app/twitch/sesame/modules/emoteplay.go). Its announcements are
  // system text, so there are no editable replies to configure.
  test('emoteplay is a toggle-only opt-in module keyed by its sesame name', () => {
    const def = moduleDef('emoteplay');
    expect(def).toBeDefined();
    expect(def?.href).toBeUndefined();
    expect(def?.hidden).toBeFalsy();
    expect(def?.toggleable).not.toBe(false);
    expect(def?.defaultEnabled).toBe(false);
    expect(def?.replies).toHaveLength(0);
    expect(def?.icon).toBe('smile');
  });

  test('govee shares Gear with Song Requests and Discord', () => {
    expect(moduleDef('govee')?.category).toBe('Gear');
    expect(moduleDef('songqueue')?.category).toBe('Gear');
    expect(moduleDef('discord')?.category).toBe('Gear');
    expect(moduleDef('discord')?.href).toBe('/discord');
    expect(moduleDef('discord')?.icon).toBe('discord');
  });

  // Discord owns a sidebar section, so it must not ALSO be a tile: it was
  // rendering in both places, which reads as two features sharing a name.
  // section is not hidden -- the row stays writable and the page reachable,
  // which is why catalogIndexable checks a separate flag.
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

  // Discord got promoted to its own dashboard section and its own delegation
  // grant (nav.ts's DASHBOARD_SECTIONS/GRANTABLE_SECTIONS), so it must NOT
  // fall back to the default ['modules'] scope any more: a pre-existing
  // 'modules' grant no longer opens /discord.
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

  test('indexable modules have unique icons so the directory is scannable', () => {
    const visible = MODULE_CATALOG.filter((def) => catalogIndexable(def));
    const icons = visible.map((def) => def.icon);
    expect(new Set(icons).size).toBe(icons.length);
  });

  test('songqueue is a bespoke href module listing !sr, !remove, !skip, !clear, !srlist and !current', () => {
    const def = moduleDef('songqueue');
    expect(def).toBeDefined();
    expect(def?.href).toBe('/songqueue');
    expect(def?.icon).toBe('music');
    expect(def?.commands?.map((c) => c.trigger)).toEqual([
      '!sr',
      '!remove',
      '!skip',
      '!clear',
      '!srlist',
      '!current'
    ]);
  });

  // The reward that queues a song is created on /songqueue, so a
  // channel-points delegate has to be able to open the page.
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
