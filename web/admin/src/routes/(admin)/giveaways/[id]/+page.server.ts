// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { allows, requireRole } from '#lib/server/access';
import { actionError, adminText, audited, okReply } from '#lib/server/admin-action';
import { giveawayGet, giveawayAlerts, giveawayPreview, giveawayFreeze, giveawayDraw, giveawayRetry, giveawayInclusion, type GiveawayAlertWire, type GiveawayDetailWire } from '#lib/server/giveaways';

function operationFields(form: FormData): { version: number; key: string } | null {
  const version = Number(String(form.get('expected_version') ?? ''));
  const key = String(form.get('idempotency_key') ?? '').trim();
  if (!Number.isSafeInteger(version) || version < 1) return null;
  if (!key) return null;
  return { version, key };
}

type DetailLoad = {
  giveaway: GiveawayDetailWire | null;
  alerts: Promise<GiveawayAlertWire[] | null>;
};

async function loadLiveDetail(actorId: string, campaignId: string): Promise<DetailLoad> {
  const campaign = giveawayGet({ actorId, campaignId }).catch(() => null);
  const unresolved = giveawayAlerts({ actorId }).catch(() => null);
  const giveaway = await campaign;
  const alerts = giveaway ? unresolved.then((rows) => campaignAlerts(giveaway, rows)) : Promise.resolve(null);
  return { giveaway, alerts };
}

function campaignAlerts(campaign: GiveawayDetailWire, rows: GiveawayAlertWire[] | null): GiveawayAlertWire[] | null {
  const awardIds = new Set(campaign.winners.map((winner) => winner.id));
  return rows?.filter((row) => awardIds.has(row.awardId)) ?? null;
}

type OperationEvent = { request: Request; locals: App.Locals; params: { id: string } };

const campaignOperations = {
  freeze: {
    run: giveawayFreeze,
    notice: 'admin.giveaway.snapshotFrozen'
  },
  draw: {
    run: giveawayDraw,
    notice: 'admin.giveaway.drawCommitted'
  }
};

async function operationContext(event: OperationEvent, needsDigest: boolean) {
  const admin = await requireRole({ locals: event.locals }, 'giveaways.manage');
  if (!admin) return { error: fail(403, { error: actionError(event.locals.locale, 'forbidden') }) } as const;
  const form = await event.request.formData();
  const digest = String(form.get('pool_digest') ?? '').trim();
  if (needsDigest && !digest) return { error: fail(400, { error: adminText(event.locals.locale, 'admin.giveaway.poolDigestRequired') }) } as const;
  const operation = operationFields(form);
  if (!operation) return { error: fail(400, { error: adminText(event.locals.locale, 'admin.giveaway.operationRequired') }) } as const;
  return { admin, digest, operation } as const;
}

async function changeCampaign(event: OperationEvent, kind: keyof typeof campaignOperations) {
  const context = await operationContext(event, kind === 'freeze');
  if ('error' in context) return context.error;
  const { admin, digest, operation } = context;
  const { run, notice } = campaignOperations[kind];
  const reply = okReply(adminText(event.locals.locale, notice));
  const target = event.params.id;
  const input = {
    actorId: admin.id, campaignId: target, poolDigest: digest,
    expectedVersion: operation.version, idempotencyKey: operation.key
  };
  return audited(
    { admin, action: `giveaway_${kind}`, target },
    async () => { await run(input); },
    () => reply
  );
}

async function inclusion(event: OperationEvent, include: boolean) {
  const admin = await requireRole({ locals: event.locals }, 'giveaways.manage');
  if (!admin) return fail(403, { error: actionError(event.locals.locale, 'forbidden') });
  const userId = String((await event.request.formData()).get('user_id') ?? '').trim();
  if (!/^\d+$/.test(userId)) return fail(400, { error: adminText(event.locals.locale, 'admin.giveaway.userIdRequired') });
  const action = include ? 'giveaway_include' : 'giveaway_exclude';
  return audited({ admin, action, target: event.params.id, detail: userId }, () => giveawayInclusion({ actorId: admin.id, campaignId: event.params.id, userId, include }), (preview) => ({ preview }));
}

export const load: PageServerLoad = async ({ params, parent, depends }) => {
  depends('admin:giveaway');
  const layout = await parent();
  if (!allows(layout.role, 'giveaways.manage')) throw redirect(302, '/');
  const id = params.id;
  return { id, ...await loadLiveDetail(layout.id, id) };
};

export const actions: Actions = {
  preview: async ({ locals, params }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    try {
      const preview = await giveawayPreview({ actorId: admin.id, campaignId: params.id });
      return { preview };
    } catch (e) {
      return fail(502, { error: (e as Error).message });
    }
  },
  freeze: (event) => changeCampaign(event, 'freeze'),
  draw: (event) => changeCampaign(event, 'draw'),
  include: (event) => inclusion(event, true),
  exclude: (event) => inclusion(event, false),
  retry: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    const awardId = String((await request.formData()).get('award_id') ?? '').trim();
    if (!awardId) return fail(400, { error: adminText(locals.locale, 'admin.giveaway.awardIdRequired') });
    return audited({ admin, action: 'giveaway_retry', target: awardId }, () => giveawayRetry({ actorId: admin.id, awardId }), () => okReply(adminText(locals.locale, 'admin.giveaway.retryQueued')));
  }
};
