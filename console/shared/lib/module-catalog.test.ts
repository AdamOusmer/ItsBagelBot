// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MODULE_CATALOG, moduleDef, moduleDelegateSections } from './types';

describe('module catalog', () => {
  test('has unique ids', () => {
    const ids = MODULE_CATALOG.map((def) => def.id);
    expect(new Set(ids).size).toBe(ids.length);
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
    expect(def?.icon).toBe('pulse');
  });
});
