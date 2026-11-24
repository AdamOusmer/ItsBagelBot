// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { error, fail, redirect } from '@sveltejs/kit';
import { RpcError } from '@bagel/kit/server/nats';
import { allows, requireRole, type AccessKey } from '#lib/server/access';
import { actionError, adminText, audited, okReply } from '#lib/server/admin-action';
import {
  campaignApprove,
  campaignAudience,
  campaignGet,
  campaignPause,
  campaignResume,
  campaignSave,
  campaignTest,
  type CampaignAudience,
  type CampaignRef
} from '#lib/server/campaigns';
import { parseCampaignDraft } from '#lib/campaign-workflow';

type ActionEvent = { request: Request; locals: App.Locals; params: { id: string } };

const FAILURE_KEYS: Record<string, string> = {
  unavailable: 'admin.campaigns.senderUnavailable',
  conflict: 'admin.campaigns.stateConflict',
  invalid: 'admin.campaigns.invalidCampaign',
  not_found: 'admin.campaigns.notFound'
};

function campaignId(raw: string): number | null {
  const id = Number(raw);
  return Number.isSafeInteger(id) && id > 0 ? id : null;
}

function readable(locale: App.Locals['locale'], failure: unknown): Error {
  const key = failure instanceof RpcError ? FAILURE_KEYS[failure.code] : undefined;
  return key ? new Error(adminText(locale, key)) : (failure as Error);
}

async function attempt(locale: App.Locals['locale'], run: () => Promise<unknown>): Promise<void> {
  try {
    await run();
  } catch (failure) {
    throw readable(locale, failure);
  }
}

const operations = {
  test: { role: 'campaigns.manage', notice: 'admin.campaigns.testSent', run: campaignTest },
  approve: { role: 'campaigns.approve', notice: 'admin.campaigns.approved', run: campaignApprove, pinned: true },
  pause: { role: 'campaigns.manage', notice: 'admin.campaigns.paused', run: campaignPause },
  resume: { role: 'campaigns.manage', notice: 'admin.campaigns.resumed', run: campaignResume }
} satisfies Record<
  string,
  { role: AccessKey; notice: string; run: (ref: CampaignRef) => Promise<unknown>; pinned?: boolean }
>;

async function reviewedRevision(request: Request): Promise<number | null> {
  const revision = Number((await request.formData()).get('revision'));
  return Number.isSafeInteger(revision) && revision > 0 ? revision : null;
}

async function operate({ request, locals, params }: ActionEvent, kind: keyof typeof operations) {
  const { role, notice, run, ...rest } = operations[kind];
  const admin = await requireRole({ locals }, role);
  if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
  const id = campaignId(params.id);
  if (!id) return fail(400, { error: actionError(locals.locale, 'campaign id required') });
  const revision = 'pinned' in rest ? await reviewedRevision(request) : undefined;
  if (revision === null) return fail(400, { error: actionError(locals.locale, 'campaign revision required') });
  return audited(
    { admin, action: `campaign_${kind}`, target: params.id },
    () => attempt(locals.locale, () => run({ actorId: admin.id, id, revision })),
    () => okReply(adminText(locals.locale, notice))
  );
}

async function save({ request, locals, params }: ActionEvent) {
  const admin = await requireRole({ locals }, 'campaigns.manage');
  if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
  const id = campaignId(params.id);
  if (!id) return fail(400, { error: actionError(locals.locale, 'campaign id required') });
  const draft = parseCampaignDraft(await request.formData());
  if ('error' in draft) return fail(400, { error: actionError(locals.locale, draft.error) });
  return audited(
    { admin, action: 'campaign_save', target: params.id },
    () => attempt(locals.locale, () => campaignSave({ actorId: admin.id, id, ...draft })),
    () => okReply(adminText(locals.locale, 'admin.campaigns.saved'))
  );
}

export const load: PageServerLoad = async ({ params, parent, depends }) => {
  depends('admin:campaign');
  const layout = await parent();
  if (!allows(layout.role, 'campaigns.manage')) throw redirect(302, '/');
  const id = campaignId(params.id);
  if (!id) throw error(404);
  const ref = { actorId: layout.id, id };
  const audience: Promise<CampaignAudience | null> = campaignAudience(ref).catch(() => null);
  const campaign = await campaignGet(ref).catch(() => null);
  return { id, campaign, audience, canApprove: allows(layout.role, 'campaigns.approve') };
};

export const actions: Actions = {
  save,
  test: (event) => operate(event, 'test'),
  approve: (event) => operate(event, 'approve'),
  pause: (event) => operate(event, 'pause'),
  resume: (event) => operate(event, 'resume')
};
