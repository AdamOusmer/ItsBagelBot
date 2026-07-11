import type { Actions, PageServerLoad } from './$types';
import type { LoyaltyConfig, LoyaltyStanding } from '@bagel/shared';
import { blankLoyaltyConfig } from '@bagel/shared';
import { readLoyalty, writeLoyalty, topStandings } from '$lib/server/loyalty-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// A delegate needs the 'modules' section; a normal login always may. Loyalty is
// a module even though it lives on its own page.
function gate(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
}

function demoStandings(): LoyaltyStanding[] {
  return [
    { viewerId: '1', viewerLogin: 'sesame_sam', viewerName: 'sesame_sam', points: 12400, watchSeconds: 90_000 },
    { viewerId: '2', viewerLogin: 'bagel_fan', viewerName: 'Bagel_Fan', points: 8300, watchSeconds: 64_800 }
  ];
}

export const load: PageServerLoad = async ({ locals }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { enabled: true, config: blankLoyaltyConfig(), top: demoStandings() };

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
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';
    if (env.DEMO === '1') return { ok: true, enabled };

    try {
      const cur = await readLoyalty(uid);
      await writeLoyalty(uid, enabled, cur.config);
    } catch (e) {
      console.error('[loyalty] toggle failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'loyalty:toggle', String(enabled));
    return { ok: true, enabled };
  },

  save: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const config = parseConfig(String(f.get('config') ?? ''));
    if (!config) return fail(400, { ok: false, error: 'Invalid settings.' });
    if (env.DEMO === '1') return { ok: true };

    try {
      const cur = await readLoyalty(uid);
      await writeLoyalty(uid, cur.enabled, config);
    } catch (e) {
      console.error('[loyalty] save failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'save failed' });
    }
    auditDashboardImpersonation(locals.session, 'loyalty:save', config.pointsName || 'points');
    return { ok: true };
  }
};
