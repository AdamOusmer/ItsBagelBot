// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { moduleEditState } from './module-edit-state';

describe('module editor revision', () => {
  test('uses the row revision after a loyalty game toggle leaves a stale mirror', () => {
    expect(moduleEditState({ name: 'duel', is_enabled: true, revision: 8, configs: { __rev: 5, minStake: 10 } }))
      .toEqual({ revision: 8, config: { minStake: '10' } });
  });

  test('reads a revision even when an upsert stores no config', () => {
    expect(moduleEditState({ name: 'duel', is_enabled: true, revision: 1 }))
      .toEqual({ revision: 1, config: {} });
  });

  test('supports legacy replies without a revision field', () => {
    expect(moduleEditState({ name: 'duel', is_enabled: false, configs: { __rev: 3 } }))
      .toEqual({ revision: 3, config: {} });
    expect(moduleEditState()).toEqual({ revision: 0, config: {} });
  });

  test('an explicit zero row revision takes precedence over the mirror', () => {
    expect(moduleEditState({ name: 'duel', is_enabled: false, revision: 0, configs: { __rev: 3 } }).revision).toBe(0);
  });
});
