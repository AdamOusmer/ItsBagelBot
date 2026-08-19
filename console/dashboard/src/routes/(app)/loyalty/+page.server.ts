// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { LoyaltyConfig, LoyaltyStanding } from '@bagel/shared';
import { blankLoyaltyConfig } from '@bagel/shared';
import { readLoyalty, writeLoyalty, topStandings } from '$lib/server/loyalty-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// The board a read/write targets. With no session there is no board: in a
// production build that is a dead end (the layout's login redirect is the only
// legitimate outcome), and only a dev demo build falls back to the fixture id.
function effectiveId(session: Session | null | undefined): string {
  const id = session?.delegate_of ?? session?.user_id;
  if (id) return id;
  if (DEMO) return 'demo';
  throw redirect(302, '/login');
}

// Delegate scope comes from the loyalty catalog def (see module-gate.ts).
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'loyalty');
}

export const load: PageServerLoad = async ({ locals }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  if (DEMO) {
    const { demoStandings } = await import('$lib/server/demo-data');
    return { enabled: true, config: blankLoyaltyConfig(), top: demoStandings() };
  }

  try {
    const view = await readLoyalty(uid);
    // The leaderboard is decorative next to the settings: a loyalty-service
    // blip must not degrade the whole page.
    let top: LoyaltyStanding[] = [];
    try {
      top = await topStandings(uid, 10);
    } catch {
      /* standings unavailable: render the settings alone */
    }
    return { enabled: view.enabled, config: view.config, top };
  } catch {
    return { enabled: false, config: blankLoyaltyConfig(), top: [] as LoyaltyStanding[], degraded: true };
  }
};

// clampRate coerces a form value into a rate: 0 = default, -1 = off, else a
// bounded positive integer.
function clampRate(raw: unknown): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n) || n === 0) return 0;
  if (n < 0) return -1;
  return Math.min(1_000_000, n);
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
    watchPointsPerTick: clampRate(obj.watchPointsPerTick)
  };
}

export const actions: Actions = {
  // Master on/off for whether anyone earns points at all.
  toggle: async ({ request, locals }) => {
    gate(locals.session);
    if (!DEMO && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });
    const uid = effectiveId(locals.session);

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';
    if (DEMO) return { ok: true, enabled };

    try {
      const cur = await readLoyalty(uid);
      await writeLoyalty(uid, enabled, cur.config);
    } catch (e) {
      logger.error({ err: e }, '[loyalty] toggle failed');
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'loyalty:toggle', String(enabled));
    return { ok: true, enabled };
  },

  save: async ({ request, locals }) => {
    gate(locals.session);
    if (!DEMO && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });
    const uid = effectiveId(locals.session);

    const f = await request.formData();
    const config = parseConfig(String(f.get('config') ?? ''));
    if (!config) return fail(400, { ok: false, error: 'Invalid settings.' });
    if (DEMO) return { ok: true };

    try {
      const cur = await readLoyalty(uid);
      await writeLoyalty(uid, cur.enabled, config);
    } catch (e) {
      logger.error({ err: e }, '[loyalty] save failed');
      return fail(400, { ok: false, error: 'save failed' });
    }
    auditDashboardImpersonation(locals.session, 'loyalty:save', config.pointsName || 'points');
    return { ok: true };
  }
};
