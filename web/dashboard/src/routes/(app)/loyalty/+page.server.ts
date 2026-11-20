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
  raffleStatus,
  resetPoints,
  topStandings,
  unfreezeStandings,
  writeLoyalty
} from '#lib/server/loyalty-store';
import { listModules, listModulesForEditing, type ModuleView } from '#lib/server/commands-store';
import { disableChildren, isChildOf } from '#lib/server/module-parent';
import { moduleLoad } from '#lib/server/module-page';
import { storeMutator } from '#lib/server/reward-action';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { fail, type Cookies } from '@sveltejs/kit';

import { gateModulePage, moduleWriteDenial } from '#lib/server/module-gate';
import { moduleEditState } from '#lib/server/module-edit-state';
import { patchModuleSettings } from '#lib/server/module-patch';
import { notSignedIn } from '#lib/server/action-context';
import { baseCookie } from '#lib/server/cookies';

const DEMO = dev && env.DEMO === '1';

const FRESH_GAME_COOKIE = 'bagel_loyalty_game_fresh';

function gameSettings(rows: ModuleView[]) {
  return catalogChildren('loyalty').map((def) => {
    const row = rows.find((r) => r.name === def.id);
    return { id: def.id, enabled: row?.is_enabled ?? def.defaultEnabled, ...moduleEditState(row) };
  });
}

function gameRows(uid: string, cookies: Cookies, url: URL) {
  if (!cookies.get(FRESH_GAME_COOKIE)) return listModules(uid);
  cookies.delete(FRESH_GAME_COOKIE, baseCookie(url, '/loyalty'));
  return listModulesForEditing(uid);
}

export const load: PageServerLoad = ({ locals, cookies, url }) =>
  moduleLoad('loyalty', locals.session, {
    demo: DEMO
      ? async () => ({
          enabled: true,
          config: blankLoyaltyConfig(),
          top: (await import('#lib/server/demo-data')).demoStandings(),
          topFreeze: null,
          freeze: null,
          raffle: null,
          games: gameSettings([])
        })
      : undefined,
    read: async (uid) => {
      const view = await readLoyalty(uid);
      const [top, freeze, modules, raffle] = await Promise.all([
        topStandings(uid).catch(() => null),
        readFreeze(uid).catch(() => null),
        gameRows(uid, cookies, url).catch(() => []),
        raffleStatus(uid).catch(() => null)
      ]);
      return { ...view, top: top?.rows ?? [], topFreeze: top?.freeze ?? null, freeze, raffle, games: gameSettings(modules) };
    },
    blank: () => ({
      enabled: false,
      config: blankLoyaltyConfig(),
      top: [] as LoyaltyStanding[],
      topFreeze: null,
      freeze: null,
      raffle: null,
      games: gameSettings([])
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

  gameSettings: async ({ request, locals, cookies, url }) => {
    gateModulePage(locals.session, 'loyalty');
    if (!DEMO && !locals.session) return notSignedIn(locals.locale);

    const form = await request.formData();
    const def = moduleDef(String(form.get('name') ?? ''));
    if (!def || !isChildOf(def, 'loyalty')) return fail(400, { ok: false });
    const denied = await moduleWriteDenial(locals, def);
    if (denied) return denied;

    const { result, freshRead } = await patchModuleSettings(def, form, locals, DEMO);
    if (freshRead) cookies.set(FRESH_GAME_COOKIE, '1', { ...baseCookie(url, '/loyalty'), maxAge: 30 });
    return result;
  }
};
