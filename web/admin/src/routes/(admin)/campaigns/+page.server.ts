// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { allows, requireRole } from '#lib/server/access';
import { actionError, adminText, audited, okReply } from '#lib/server/admin-action';
import { campaignList, campaignSave, type CampaignWire } from '#lib/server/campaigns';
import { parseCampaignName } from '#lib/campaign-workflow';

export const load: PageServerLoad = async ({ parent, depends }) => {
  depends('admin:campaigns');
  const layout = await parent();
  if (!allows(layout.role, 'campaigns.manage')) throw redirect(302, '/');

  const history = campaignList(layout.id)
    .then((campaigns) => ({ campaigns, degraded: false }))
    .catch(() => ({ campaigns: [] as CampaignWire[], degraded: true }));
  return { history };
};

export const actions: Actions = {
  create: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'campaigns.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    const name = parseCampaignName((await request.formData()).get('name'));
    if (typeof name !== 'string') return fail(400, { error: actionError(locals.locale, name.error) });
    return audited(
      { admin, action: 'campaign_create', target: name },
      () => campaignSave({ actorId: admin.id, name, content: {} }),
      (created) => ({ ...okReply(adminText(locals.locale, 'admin.campaigns.created')), campaign: { id: created.id } })
    );
  }
};
