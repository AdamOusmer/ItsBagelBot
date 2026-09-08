// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { LoyaltyConfig, LoyaltyStanding } from '@bagel/shared';
import { blankLoyaltyConfig, catalogChildren, moduleDef } from '@bagel/shared';
import { readLoyalty, writeLoyalty, topStandings } from '$lib/server/loyalty-store';
import { listModules, upsertModule } from '$lib/server/commands-store';
import { disableChildren, isChildOf } from '$lib/server/module-parent';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction, type ModuleMutation } from '$lib/server/module-action';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
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
      // The leaderboard is decorative next to the settings: a loyalty-service
      // blip must not degrade the whole page.
      let top: LoyaltyStanding[] = [];
      try {
        top = await topStandings(uid, 10);
      } catch {
        /* standings unavailable: render the settings alone */
      }
      let games = gameFlags([]);
      try {
        games = gameFlags(await listModules(uid));
      } catch {
        /* nested game flags unavailable: render the rows off */
      }
      return { enabled: view.enabled, config: view.config, top, games };
    },
    blank: () => ({
      enabled: false,
      config: blankLoyaltyConfig(),
      top: [] as LoyaltyStanding[],
      games: gameFlags([])
    })
  });

// clampRate coerces a form value into a rate: 0 = default, -1 = off, else a
// bounded positive integer.
function clampRate(raw: unknown): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n) || n === 0) return 0;
  if (n < 0) return -1;
  return Math.min(1_000_000, n);
}

// permValue coerces a permission field into the blob's convention:
// 0 (or any non-negative) = on, negative = off. Read from the posted JSON
// draft, not from missing form checkboxes: the switches write into config
// and the hidden `config` field is the only thing that posts.
function permValue(raw: unknown): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n) || n >= 0) return 0;
  return -1;
}

// parseConfig validates the posted rates JSON into a full LoyaltyConfig.
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

// mutate binds one POST action to the module write skeleton
// ($lib/server/module-action): delegate gate, form, demo short-circuit, error
// mapping, audit. Each verb below is only its own parse plus its store call.
function mutate(op: string, invalid: string, run: ModuleMutation) {
  return moduleAction('loyalty', op, run, { demo: DEMO, invalid });
}

export const actions: Actions = {
  // Master on/off for whether anyone earns points at all.
  toggle: mutate('toggle', 'Invalid settings.', async (uid, f) => {
    const enabled = f.get('is_enabled') === 'on';
    const cur = await readLoyalty(uid);
    await writeLoyalty(uid, enabled, cur.config);
    // Wager games cannot run without the currency. Turning loyalty off
    // turns them off too, so a later re-enable of loyalty does not
    // silently resurrect !gamble.
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

  // Nested game on/off. Refuses to enable while loyalty is off so a forged
  // POST cannot arm !gamble against a channel that has not turned the
  // currency on. The child's stored config is preserved across the flip.
  toggleGame: mutate('toggleGame', 'Unknown game.', async (uid, f) => {
    const name = String(f.get('name') ?? '');
    const enabled = f.get('is_enabled') === 'on';
    if (!isChildOf(moduleDef(name), 'loyalty')) return null;
    const loy = await readLoyalty(uid);
    // A reason the page renders (it points at the master switch), not a fault.
    if (enabled && !loy.enabled) return fail(400, { ok: false, error: 'loyalty-off' });
    const rows = await listModules(uid);
    const config = rows.find((r) => r.name === name)?.configs;
    await upsertModule(uid, name, enabled, config);
    return `${name}=${enabled}`;
  })
};
