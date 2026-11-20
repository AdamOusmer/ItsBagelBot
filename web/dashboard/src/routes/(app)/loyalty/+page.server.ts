// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { LoyaltyConfig, LoyaltyStanding } from '@bagel/kit';
import { blankLoyaltyConfig, catalogChildren, moduleDef } from '@bagel/kit';
import { FREEZE_MAX_MINUTES } from '@bagel/kit/catalog';
import { RpcError } from '@bagel/kit/server/nats';
import {
  freezeStandings,
  readFreeze,
  readLoyalty,
  resetPoints,
  topStandings,
  unfreezeStandings,
  writeLoyalty
} from '#lib/server/loyalty-store';
import { listModules } from '#lib/server/commands-store';
import { setModuleEnabled } from '#lib/server/module-blob';
import { disableChildren, isChildOf } from '#lib/server/module-parent';
import { moduleLoad } from '#lib/server/module-page';
import { storeMutator } from '#lib/server/reward-action';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { fail } from '@sveltejs/kit';

const DEMO = dev && env.DEMO === '1';

function gameFlags(rows: { name: string; is_enabled: boolean }[]) {
  return catalogChildren('loyalty').map((def) => ({
    id: def.id,
    enabled: rows.find((r) => r.name === def.id)?.is_enabled ?? false
  }));
}

export const load: PageServerLoad = ({ locals }) =>
  moduleLoad('loyalty', locals.session, {
    demo: DEMO
      ? async () => ({
          enabled: true,
          config: blankLoyaltyConfig(),
          top: (await import('#lib/server/demo-data')).demoStandings(),
          topFreeze: null,
          freeze: null,
          games: gameFlags([])
        })
      : undefined,
    read: async (uid) => {
      const view = await readLoyalty(uid);
      const [top, freeze, modules] = await Promise.all([
        topStandings(uid).catch(() => null),
        readFreeze(uid).catch(() => null),
        listModules(uid).catch(() => [])
      ]);
      return { ...view, top: top?.rows ?? [], topFreeze: top?.freeze ?? null, freeze, games: gameFlags(modules) };
    },
    blank: () => ({
      enabled: false,
      config: blankLoyaltyConfig(),
      top: [] as LoyaltyStanding[],
      topFreeze: null,
      freeze: null,
      games: gameFlags([])
    })
  });

function clampRate(raw: unknown): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n) || n === 0) return 0;
  if (n < 0) return -1;
  return Math.min(1_000_000, n);
}

function permValue(raw: unknown): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n) || n >= 0) return 0;
  return -1;
}

function tierPercent(raw: unknown): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n) || n === 0) return 0;
  return Math.min(1000, Math.max(100, n));
}

function parseConfig(raw: string): LoyaltyConfig | null {
  let obj: Partial<LoyaltyConfig>;
  try {
    obj = JSON.parse(raw) as Partial<LoyaltyConfig>;
  } catch {
    return null;
  }
  const pointsName = String(obj.pointsName ?? '')
    .trim()
    .slice(0, 32);
  return {
    pointsName,
    subPoints: clampRate(obj.subPoints),
    resubPoints: clampRate(obj.resubPoints),
    giftSubPoints: clampRate(obj.giftSubPoints),
    cheerPointsPer100: clampRate(obj.cheerPointsPer100),
    watchPointsPerTick: clampRate(obj.watchPointsPerTick),
    modSetPoints: permValue(obj.modSetPoints),
    modAdjustPoints: permValue(obj.modAdjustPoints),
    viewerTransfers: permValue(obj.viewerTransfers),
    streamerPoints: permValue(obj.streamerPoints),
    watchSubBonus: obj.watchSubBonus === true,
    watchTier1Percent: tierPercent(obj.watchTier1Percent),
    watchTier2Percent: tierPercent(obj.watchTier2Percent),
    watchTier3Percent: tierPercent(obj.watchTier3Percent)
  };
}

const mutate = storeMutator('loyalty', DEMO, (uid) => uid);

function freezeConflict(err: unknown, reason: 'frozen' | 'held_by_round') {
  if (!(err instanceof RpcError) || err.code !== 'conflict') throw err;
  return fail(409, { ok: false, reason });
}

function validFreezeMinutes(minutes: number): boolean {
  return Number.isInteger(minutes) && minutes >= 1 && minutes <= FREEZE_MAX_MINUTES;
}

export const actions: Actions = {
  toggle: mutate('toggle', 'Invalid settings.', async (uid, f) => {
    const enabled = f.get('is_enabled') === 'on';
    const cur = await readLoyalty(uid);
    await writeLoyalty(uid, enabled, cur.config);
    if (!enabled) await disableChildren(uid, 'loyalty');
    return String(enabled);
  }),

  save: mutate('save', 'Invalid settings.', async (uid, f) => {
    const config = parseConfig(String(f.get('config') ?? ''));
    if (!config) return null;
    const cur = await readLoyalty(uid);
    await writeLoyalty(uid, cur.enabled, config);
    return config.pointsName || 'points';
  }),

  reset: mutate('reset', 'Invalid request.', async (uid, f) => {
    if (f.get('confirm') !== 'reset') return null;
    try {
      return String(await resetPoints(uid));
    } catch (err) {
      return freezeConflict(err, 'frozen');
    }
  }),

  freeze: mutate('freeze', 'Invalid request.', async (uid, f) => {
    const minutes = Number(f.get('minutes'));
    if (!validFreezeMinutes(minutes)) return null;
    const freeze = await freezeStandings(uid, {
      minutes,
      blockTransfer: f.get('block_transfer') === 'on',
      closeGames: f.get('close_games') === 'on'
    });
    if (!freeze) throw new Error('empty freeze reply');
    return freeze.id;
  }),

  unfreeze: mutate('unfreeze', 'Invalid request.', async (uid, f) => {
    const id = f.get('freeze_id');
    if (typeof id !== 'string' || !id.trim()) return null;
    try {
      const released = await unfreezeStandings(uid, id);
      return released ? id : fail(409, { ok: false, reason: 'stale_pin' });
    } catch (err) {
      return freezeConflict(err, 'held_by_round');
    }
  }),

  toggleGame: mutate('toggleGame', 'Unknown game.', async (uid, f) => {
    const name = String(f.get('name') ?? '');
    const enabled = f.get('is_enabled') === 'on';
    if (!isChildOf(moduleDef(name), 'loyalty')) return null;
    const loy = await readLoyalty(uid);
    if (enabled && !loy.enabled) return fail(400, { ok: false, error: 'loyalty-off' });
    await setModuleEnabled(uid, name, enabled);
    return `${name}=${enabled}`;
  })
};
