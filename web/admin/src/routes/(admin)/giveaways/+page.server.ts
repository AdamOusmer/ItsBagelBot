// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { allows, requireRole } from '#lib/server/access';
import { audited, okReply, actionError, adminText } from '#lib/server/admin-action';
import { giveawayCreate, giveawayPreview, giveawaysList } from '#lib/server/giveaways';
import { parsePrizeMonths, parseWinnerCount } from '@bagel/kit/giveaway';

const DEMO = dev && process.env.DEMO === '1';

export const load: PageServerLoad = async ({ parent, url, depends }) => {
  depends('admin:giveaways');
  const layout = await parent();
  if (!allows(layout.role, 'giveaways.manage')) throw redirect(302, '/');

  const demoStatus = DEMO ? url.searchParams.get('demo') : null;
  const history = DEMO
    ? import('#lib/server/demo-giveaways').then(({ demoGiveawayHistory }) => demoGiveawayHistory(demoStatus))
    : giveawaysList({ actorId: layout.id })
        .then((giveaways) => ({ giveaways, degraded: false }))
        .catch(() => ({ giveaways: [], degraded: true }));
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
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    const parsed = fields(await request.formData());
    if ('error' in parsed) return fail(400, { error: actionError(locals.locale, parsed.error) });
    if (DEMO) {
      const { demoGiveawayDraftPreview } = await import('#lib/server/demo-giveaways');
      return { preview: demoGiveawayDraftPreview(parsed.winnerCount, parsed.prizeMonths) };
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
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    const parsed = fields(await request.formData());
    if ('error' in parsed) return fail(400, { error: actionError(locals.locale, parsed.error) });
    if (DEMO) return { ...okReply(adminText(locals.locale, 'admin.giveaway.draftCreatedDemo')), giveaway: { id: 'demo-draft' } };
    return audited(
      { admin, action: 'giveaway_create', target: parsed.title, detail: `${parsed.winnerCount}×${parsed.prizeMonths}` },
      () => giveawayCreate({ actorId: admin.id, ...parsed }),
      (created) => ({ ...okReply(adminText(locals.locale, 'admin.giveaway.draftCreated')), giveaway: created })
    );
  }
};
