// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

mock.module('./edge-purge', () => ({ schedulePurgeChannel: () => {}, scheduleLeaderboardPurge: () => {} }));

let row: { name: string; is_enabled: boolean; revision: number; configs: unknown } | undefined;
const stored = () => (row ? [row] : []);
const nats = await import('@bagel/kit/server/nats');
const { SUB } = await import('./services');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, req: { name: string; is_enabled: boolean; configs?: unknown }) => {
    if (subject === `${SUB.modules}.upsert`) {
      row = { name: req.name, is_enabled: req.is_enabled, revision: 1, configs: req.configs ?? {} };
      return { modules: stored() };
    }
    if (subject === `${SUB.projector}.modules.replace` || subject === `${SUB.projector}.modules.get`) return { modules: stored() };
    throw new Error(`Unexpected RPC: ${subject}`);
  }
}));

const valkey = await import('@bagel/kit/server/valkey-store');
mock.module('@bagel/kit/server/valkey-store', () => ({ ...valkey, getModules: async () => ({ projected: true, modules: stored() }) }));

const { readLoyalty, writeLoyalty } = await import('./loyalty-store');
const { blankLoyaltyConfig, toResetSchedule } = await import('@bagel/kit');

describe('loyalty reset schedule', () => {
  test.each(['off', 'weekly', 'monthly', 'annually'] as const)('%s survives a write and a read', async (resetSchedule) => {
    await writeLoyalty('7', true, { ...blankLoyaltyConfig(), pointsName: 'bagels', resetSchedule });
    const view = await readLoyalty('7');
    expect(view.config.resetSchedule).toBe(resetSchedule);
    expect(view.config.pointsName).toBe('bagels');
  });

  test('a stored config without the key reads as off', async () => {
    row = { name: 'loyalty', is_enabled: true, revision: 1, configs: { pointsName: 'bagels' } };
    expect((await readLoyalty('8')).config.resetSchedule).toBe('off');
  });

  test.each(['daily', '', 3, null, undefined])('%p is not a schedule', (raw) => {
    expect(toResetSchedule(raw)).toBe('off');
  });
});
