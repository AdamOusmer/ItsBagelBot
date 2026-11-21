// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { allows, requireRole } from '#lib/server/access';
import { actionError, adminText, audited, okReply } from '#lib/server/admin-action';
import { giveawayGet, giveawayAlerts, giveawayPreview, giveawayFreeze, giveawayDraw, giveawayRetry, giveawayInclusion, type GiveawayDetailWire } from '#lib/server/giveaways';

const DEMO = dev && process.env.DEMO === '1';

function operationFields(form: FormData): { version: number; key: string } | null {
  const version = Number(String(form.get('expected_version') ?? ''));
  const key = String(form.get('idempotency_key') ?? '').trim();
  if (!Number.isSafeInteger(version) || version < 1) return null;
  if (!key) return null;
  return { version, key };
}

type DetailLoad = { giveaway: GiveawayDetailWire | null; degraded: boolean; alertsDegraded: boolean };

async function loadLiveDetail(actorId: string, campaignId: string): Promise<DetailLoad> {
  try {
    const [giveaway, alerts] = await Promise.all([
      giveawayGet({ actorId, campaignId }),
      giveawayAlerts({ actorId }).catch(() => null)
    ]);
    const awardIds = new Set(giveaway.winners.map((winner) => winner.id));
    return {
      giveaway: { ...giveaway, alerts: alerts?.filter((alert) => awardIds.has(alert.awardId)) ?? [] },
      degraded: false,
      alertsDegraded: alerts === null
    };
  } catch {
    return { giveaway: null, degraded: true, alertsDegraded: false };
  }
}

type OperationEvent = { request: Request; locals: App.Locals; params: { id: string } };

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

async function inclusion(event: OperationEvent, include: boolean) {
  const admin = await requireRole({ locals: event.locals }, 'giveaways.manage');
  if (!admin) return fail(403, { error: actionError(event.locals.locale, 'forbidden') });
  const userId = String((await event.request.formData()).get('user_id') ?? '').trim();
  if (!/^\d+$/.test(userId)) return fail(400, { error: adminText(event.locals.locale, 'admin.giveaway.userIdRequired') });
  if (DEMO) {
    const { demoGiveawayPreview } = await import('#lib/server/demo-giveaways');
    return { preview: demoGiveawayPreview() };
  }
  const action = include ? 'giveaway_include' : 'giveaway_exclude';
  return audited({ admin, action, target: event.params.id, detail: userId }, () => giveawayInclusion({ actorId: admin.id, campaignId: event.params.id, userId, include }), (preview) => ({ preview }));
}

export const load: PageServerLoad = async ({ params, parent, url }) => {
  const layout = await parent();
  if (!allows(layout.role, 'giveaways.manage')) throw redirect(302, '/');
  const campaignId = params.id;
  const demoStatus = DEMO ? url.searchParams.get('demo') : null;
  const detail: Promise<DetailLoad> = DEMO
    ? import('#lib/server/demo-giveaways').then(({ demoGiveawayDetail }) => demoGiveawayDetail(campaignId, demoStatus))
    : loadLiveDetail(layout.id, campaignId);
  return { detail };
};

export const actions: Actions = {
  preview: async ({ locals, params }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    if (DEMO) {
      const { demoGiveawayPreview } = await import('#lib/server/demo-giveaways');
      return { preview: demoGiveawayPreview() };
    }
    try {
      const preview = await giveawayPreview({ actorId: admin.id, campaignId: params.id });
      return { preview };
    } catch (e) {
      return fail(502, { error: (e as Error).message });
    }
  },
  freeze: async (event) => {
    const context = await operationContext(event, true);
    if ('error' in context) return context.error;
    const { admin, digest, operation } = context;
    if (DEMO) return okReply(adminText(event.locals.locale, 'admin.giveaway.snapshotFrozenDemo'));
    return audited({ admin, action: 'giveaway_freeze', target: event.params.id }, () => giveawayFreeze({ actorId: admin.id, campaignId: event.params.id, poolDigest: digest, expectedVersion: operation.version, idempotencyKey: operation.key }), () => okReply(adminText(event.locals.locale, 'admin.giveaway.snapshotFrozen')));
  },
  draw: async (event) => {
    const context = await operationContext(event, false);
    if ('error' in context) return context.error;
    const { admin, operation } = context;
    if (DEMO) return okReply(adminText(event.locals.locale, 'admin.giveaway.drawCommittedDemo'));
    return audited({ admin, action: 'giveaway_draw', target: event.params.id }, () => giveawayDraw({ actorId: admin.id, campaignId: event.params.id, expectedVersion: operation.version, idempotencyKey: operation.key }), () => okReply(adminText(event.locals.locale, 'admin.giveaway.drawCommitted')));
  },
  include: (event) => inclusion(event, true),
  exclude: (event) => inclusion(event, false),
  retry: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    const awardId = String((await request.formData()).get('award_id') ?? '').trim();
    if (!awardId) return fail(400, { error: adminText(locals.locale, 'admin.giveaway.awardIdRequired') });
    if (DEMO) return okReply(adminText(locals.locale, 'admin.giveaway.retryQueuedDemo'));
    return audited({ admin, action: 'giveaway_retry', target: awardId }, () => giveawayRetry({ actorId: admin.id, awardId }), () => okReply(adminText(locals.locale, 'admin.giveaway.retryQueued')));
  }
};
