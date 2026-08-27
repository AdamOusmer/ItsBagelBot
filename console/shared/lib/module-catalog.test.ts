// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MOD, MODULE_CATALOG, catalogIndexable, moduleDef, moduleDelegateSections } from './types';

describe('module catalog', () => {
  test('has unique ids', () => {
    const ids = MODULE_CATALOG.map((def) => def.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  // MOD is a hand-written id map sitting next to 21 independently-declared
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

  test('folds counters into Modules delegation without an enable switch', () => {
    const counters = moduleDef('counters');
    expect(counters).toBeDefined();
    expect(counters?.href).toBe('/counters');
    expect(counters?.toggleable).toBe(false);
    expect(moduleDelegateSections(counters!)).toEqual(['modules']);
  });

  // The emoteplay tile must stay a plain opt-in toggle: no bespoke page, no
  // delegation grant of its own, and an id matching the sesame module name the
  // engine gates on (app/sesame/modules/emoteplay.go). Its announcements are
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

  test('govee shares Gear with Song Requests', () => {
    expect(moduleDef('govee')?.category).toBe('Gear');
    expect(moduleDef('songqueue')?.category).toBe('Gear');
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
