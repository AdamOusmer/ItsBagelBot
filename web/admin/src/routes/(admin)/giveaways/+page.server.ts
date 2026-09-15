import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { allows, requireRole } from '$lib/server/access';
import { audited, okReply } from '$lib/server/admin-action';
import { giveawayCreate, giveawayDraw, giveawayPreview, giveawaysList, type GiveawayWire } from '$lib/server/giveaways';
import { giveawaySummary, parsePrizeMonths, parseWinnerCount } from '@bagel/kit/giveaway';

const DEMO = dev && process.env.DEMO === '1';

const demoGiveaways: GiveawayWire[] = [
  {
    id: 'demo-september', title: 'September community giveaway', reason: 'Launch celebration', status: 'complete', winnerCount: 2, prizeMonths: 3,
    summary: { winners: 2, months: 3, totalMonths: 6 }, createdAt: '2026-09-01T12:00:00Z', drawnAt: '2026-09-03T12:00:00Z', pendingAwards: 0, version: 2
  }
];

export const load: PageServerLoad = async ({ parent, url }) => {
  const layout = await parent();
  if (!allows(layout.role, 'giveaways.manage')) throw redirect(302, '/');
  const demoStatus: GiveawayWire['status'] | null = url.searchParams.get('demo') === 'draft' ? 'draft' : url.searchParams.get('demo') === 'frozen' ? 'frozen' : null;
  const demoHistory = demoStatus ? demoGiveaways.map((giveaway) => ({ ...giveaway, status: demoStatus })) : demoGiveaways;
  const history: Promise<{ giveaways: GiveawayWire[]; degraded: boolean }> = DEMO
    ? Promise.resolve({ giveaways: demoHistory, degraded: false })
    : giveawaysList({ actorId: layout.id }).then((giveaways) => ({ giveaways, degraded: false })).catch(() => ({ giveaways: [], degraded: true }));
  return { history };
};

type GiveawayForm = { title: string; reason: string; winnerCount: number; prizeMonths: number };

function fields(form: FormData): GiveawayForm | { error: string } {
  const title = String(form.get('title') ?? '').trim().slice(0, 160);
  const reason = String(form.get('reason') ?? '').trim().slice(0, 500);
  const winnerCount = parseWinnerCount(form.get('winner_count'));
  const prizeMonths = parsePrizeMonths(form.get('prize_months'));
  if (!title) return { error: 'title required' };
  if (!reason) return { error: 'internal reason required' };
  if (!winnerCount) return { error: 'winner count must be a positive whole number' };
  if (!prizeMonths) return { error: 'prize duration must be a whole number from 1 to 12' };
  return { title, reason, winnerCount, prizeMonths };
}

export const actions: Actions = {
  preview: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: 'forbidden' });
    const parsed = fields(await request.formData());
    if ('error' in parsed) return fail(400, parsed);
    if (DEMO) {
      return {
        preview: {
          eligible: { eligible: 42, free: 25, premium: 9, subscribers: 8, excluded: 17 },
          exclusions: { banned: 0, inactive: 6, not_onboarded: 2, test_account: 4, vip: 3, current_staff: 2 },
          summary: giveawaySummary(parsed.winnerCount, parsed.prizeMonths),
          capabilities: { newAwardsEnabled: true, schedulingEnabled: false, providerMutationsEnabled: false, intervalRuleVerified: false, reason: 'demo pending protection' }
        }
      };
    }
    try {
      const preview = await giveawayPreview({ actorId: admin.id, winnerCount: parsed.winnerCount, prizeMonths: parsed.prizeMonths });
      return { preview };
    } catch (e) {
      return fail(502, { error: (e as Error).message });
    }
  },

  create: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: 'forbidden' });
    const parsed = fields(await request.formData());
    if ('error' in parsed) return fail(400, parsed);
    if (DEMO) return okReply('Giveaway draft created (demo).');
    return audited(
      { admin, action: 'giveaway_create', target: parsed.title, detail: `${parsed.winnerCount}×${parsed.prizeMonths}` },
      () => giveawayCreate({ actorId: admin.id, ...parsed }),
      (created) => ({ ...okReply('Giveaway draft created.'), giveaway: created })
    );
  },

  draw: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: 'forbidden' });
    const id = String((await request.formData()).get('giveaway_id') ?? '').trim();
    if (!id) return fail(400, { error: 'giveaway id required' });
    if (DEMO) return okReply('Draw committed (demo).');
    return audited(
      { admin, action: 'giveaway_draw', target: id },
      () => giveawayDraw({ actorId: admin.id, campaignId: id, expectedVersion: 1, idempotencyKey: `giveaway:${id}:draw` }),
      (detail) => ({ ...okReply('Draw committed.'), detail })
    );
  }
};
