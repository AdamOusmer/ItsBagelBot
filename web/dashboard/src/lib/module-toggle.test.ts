// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import type { ModuleState } from '@bagel/kit';
import { reconcileModuleToggles } from './module-toggle';

const row = (id: string, enabled: boolean, revision: number): ModuleState => ({
  def: { id } as ModuleState['def'], enabled, revision, config: { message: String(revision) }
});

test('refresh during a toggle preserves its intent and refreshes other modules', () => {
  const result = reconcileModuleToggles(
    [row('queue', false, 7), row('welcome', true, 3)],
    [row('queue', true, 7), row('welcome', false, 2)],
    new Map([['queue', true]])
  );
  expect(result.map((r) => r.enabled)).toEqual([true, true]);
});

test('a refresh dispatched before the save cannot roll back its acknowledged revision', () => {
  const current = [row('queue', true, 8)];
  const stale = reconcileModuleToggles([row('queue', false, 7)], current, new Map());
  expect(stale).toEqual(current);
  const newer = reconcileModuleToggles([row('queue', false, 9)], stale, new Map());
  expect(newer).toEqual([row('queue', false, 9)]);
});

test('failed toggles can roll back and follow authoritative refreshes', () => {
  const rolledBack = [row('queue', false, 7)];
  expect(reconcileModuleToggles(rolledBack, rolledBack, new Map())).toEqual(rolledBack);
});
