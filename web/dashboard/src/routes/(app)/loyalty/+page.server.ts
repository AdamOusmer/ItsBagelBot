// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { LoyaltyConfig, LoyaltyStanding } from '@bagel/kit';
import { blankLoyaltyConfig, catalogChildren, moduleDef } from '@bagel/kit';
import { readLoyalty, writeLoyalty, topStandings } from '$lib/server/loyalty-store';
import { listModules } from '$lib/server/commands-store';
import { setModuleEnabled } from '$lib/server/module-blob';
import { disableChildren, isChildOf } from '$lib/server/module-parent';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction, type ModuleMutation } from '$lib/server/module-action';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
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
          top: (await import('$lib/server/demo-data')).demoStandings(),
          games: gameFlags([])
        })
      : undefined,
    read: async (uid) => {
      const view = await readLoyalty(uid);
      let top: LoyaltyStanding[] = [];
      try {
        top = await topStandings(uid, 10);
      } catch {}
      let games = gameFlags([]);
      try {
        games = gameFlags(await listModules(uid));
      } catch {}
      return { enabled: view.enabled, config: view.config, top, games };
    },
    blank: () => ({
      enabled: false,
      config: blankLoyaltyConfig(),
      top: [] as LoyaltyStanding[],
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
    viewerTransfers: permValue(obj.viewerTransfers)
  };
}

function mutate(op: string, invalid: string, run: ModuleMutation) {
  return moduleAction('loyalty', op, run, { demo: DEMO, invalid });
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
