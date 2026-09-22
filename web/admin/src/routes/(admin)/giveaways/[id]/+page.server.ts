import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { allows, requireRole } from '$lib/server/access';
import { audited, okReply } from '$lib/server/admin-action';
import { actionError, adminText } from '$lib/server/admin-action';
import { giveawayGet, giveawayAlerts, giveawayPreview, giveawayFreeze, giveawayDraw, giveawayRetry, type GiveawayDetailWire, type GiveawayPreviewWire } from '$lib/server/giveaways';

const DEMO = dev && process.env.DEMO === '1';

function operationFields(form: FormData): { version: number; key: string } | { error: string } {
  const version = Number(String(form.get('expected_version') ?? ''));
  const key = String(form.get('idempotency_key') ?? '').trim();
  if (!Number.isSafeInteger(version) || version < 1) return { error: 'snapshot version and operation key required' };
  if (!key) return { error: 'snapshot version and operation key required' };
  return { version, key };
}

function demoDetail(id: string, status: GiveawayDetailWire['status'] = 'drawn'): GiveawayDetailWire {
  const detail: GiveawayDetailWire = {
    id, title: 'September community giveaway', reason: 'Launch celebration', status, winnerCount: 2, prizeMonths: 3,
    createdAt: '2026-09-01T12:00:00Z', drawnAt: '2026-09-03T12:00:00Z', pendingAwards: 1, version: 2,
    eligible: { eligible: 42, free: 25, premium: 9, subscribers: 8, excluded: 17 }, selectionMethod: 'random_draw', algorithmVersion: 'crypto-rand-partial-fisher-yates-v1', poolDigest: 'demo', capabilities: { newAwardsEnabled: true, schedulingEnabled: false, providerMutationsEnabled: false, intervalRuleVerified: false, reason: 'demo pending protection' },
    winners: [
      { id: 'award-1', userId: '1001', login: 'bagel_friend', selectedAt: '2026-09-03T12:00:00Z', prizeMonths: 3, awardState: 'completed', billingState: 'not_required', startAt: '2026-09-03T12:00:00Z', endAt: '2026-12-03T12:00:00Z', paidThroughAt: null, nextChargeAt: null, providerVerifiedAt: null, emailState: 'accepted', emailWarning: null, failureReason: null, retryCount: 0 },
      { id: 'award-2', userId: '1002', login: 'monthly_winner', selectedAt: '2026-09-03T12:00:00Z', prizeMonths: 3, awardState: 'needs_review', billingState: 'pending', startAt: null, endAt: null, paidThroughAt: null, nextChargeAt: null, providerVerifiedAt: null, emailState: 'missing_contact', emailWarning: 'No contact email', failureReason: 'Provider pause not verified', retryCount: 1 }
    ], alerts: [{ id: 'alert-1', awardId: 'award-2', reason: 'Provider pause not verified', pendingSince: '2026-09-03T12:00:00Z', upcomingChargeAt: '2026-09-20T12:00:00Z', urgent: true }]
  };
  if (status === 'drawn' || status === 'complete') return detail;
  return { ...detail, drawnAt: null, pendingAwards: 0, winners: [], alerts: [] };
}

function demoPreview(id: string): GiveawayPreviewWire {
  const detail = demoDetail(id);
  return {
    eligible: detail.eligible ?? { eligible: 0, free: 0, premium: 0, subscribers: 0, excluded: 0 },
    exclusions: { banned: 0, inactive: 6, not_onboarded: 2, test_account: 4, vip: 3, current_staff: 2 },
    summary: { winners: detail.winnerCount, months: detail.prizeMonths, totalMonths: detail.winnerCount * detail.prizeMonths },
    poolDigest: 'demo-preview', capabilities: detail.capabilities
  };
}

type DetailLoad = { giveaway: GiveawayDetailWire | null; degraded: boolean; alertsDegraded: boolean };

async function loadLiveDetail(actorId: string, campaignId: string): Promise<DetailLoad> {
  try {
    const giveaway = await giveawayGet({ actorId, campaignId });
    let alerts: Awaited<ReturnType<typeof giveawayAlerts>> = [];
    let alertsDegraded = false;
    try {
      alerts = await giveawayAlerts({ actorId });
    } catch {
      alertsDegraded = true;
    }
    const awardIds = new Set(giveaway.winners.map((winner) => winner.id));
    return { giveaway: { ...giveaway, alerts: alerts.filter((alert) => awardIds.has(alert.awardId)) }, degraded: false, alertsDegraded };
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
  if ('error' in operation) return { error: fail(400, { error: actionError(event.locals.locale, operation.error) }) } as const;
  return { admin, digest, operation } as const;
}

export const load: PageServerLoad = async ({ params, parent, url }) => {
  const layout = await parent();
  if (!allows(layout.role, 'giveaways.manage')) throw redirect(302, '/');
  const id = params.id;
  const demoStatus = url.searchParams.get('demo') === 'draft' ? 'draft' : url.searchParams.get('demo') === 'frozen' ? 'frozen' : 'drawn';
  const detail: Promise<DetailLoad> = DEMO
    ? Promise.resolve({ giveaway: demoDetail(id, demoStatus), degraded: false, alertsDegraded: false })
    : loadLiveDetail(layout.id, id);
  return { detail };
};

export const actions: Actions = {
  preview: async ({ locals, params }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    if (DEMO) return { preview: demoPreview(params.id) };
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
  retry: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'giveaways.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });
    const awardId = String((await request.formData()).get('award_id') ?? '').trim();
    if (!awardId) return fail(400, { error: adminText(locals.locale, 'admin.giveaway.awardIdRequired') });
    if (DEMO) return okReply(adminText(locals.locale, 'admin.giveaway.retryQueuedDemo'));
    return audited({ admin, action: 'giveaway_retry', target: awardId }, () => giveawayRetry({ actorId: admin.id, awardId }), () => okReply(adminText(locals.locale, 'admin.giveaway.retryQueued')));
  }
};
