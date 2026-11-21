// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, it, mock, spyOn } from 'bun:test';
import { staticText } from '@bagel/kit/i18n/static';
import { actionPayload } from '../../kit/lib/action-result';
import { allows } from '../src/lib/access';
import { freezePoolDigest, giveawayExclusionLabel } from '../src/lib/giveaway-workflow';
import type { GiveawayAlertWire, GiveawayDetailWire } from '../src/lib/server/giveaways';
import { stubSvelteKit } from './sveltekit';

stubSvelteKit();

mock.module('virtual:bagel-i18n', () => ({ locales: ['en', 'fr'], urls: {}, loaders: {}, localeNames: {}, sliced: false }));
const i18n = await import('@bagel/kit/i18n');
const access = await import('../src/lib/server/access');
const audit = await import('../src/lib/server/audit');
const nats = await import('@bagel/kit/server/nats');
const allowsForRole = allows;

const requests: { subject: string; body: unknown }[] = [];
const replies = new Map<string, unknown>();
let reply: unknown = {};
const admin = { id: 'admin-1', login: 'mod', display_name: 'Mod', role: 'admin' } as const;
let admitted = true;

async function rpcReply<T>(subject: string, body: unknown): Promise<T> {
  requests.push({ subject, body });
  const response = replies.get(subject) ?? reply;
  if (response instanceof Error) throw response;
  return response as T;
}

function stubDependencies() {
  return [
    spyOn(nats, 'rpc').mockImplementation(rpcReply),
    spyOn(access, 'allows').mockImplementation(allowsForRole),
    spyOn(access, 'requireRole').mockImplementation(async () => admitted ? admin : null),
    spyOn(audit, 'audit').mockImplementation(() => {}),
    spyOn(i18n, 'translate').mockImplementation((locale, key, params = {}) =>
      Object.entries(params).reduce((text, [name, value]) => text.replaceAll(`{${name}}`, String(value)), staticText(locale, key)))
  ];
}

let spies: ReturnType<typeof stubDependencies>;

const { giveawayFreeze, giveawayGet, giveawayInclusion, giveawayPreview } = await import('../src/lib/server/giveaways');
const { load: loadGiveaway } = await import('../src/routes/(admin)/giveaways/[id]/+page.server');
const { actions: trialActions } = await import('../src/routes/(admin)/trials/+page.server');

const campaign = {
  id: 'c-1', title: 'Launch', reason: 'Internal', rules_version: 'v1', winner_count: 2, prize_months: 3,
  status: 'frozen', created_by: 7, version: 4, created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-02T00:00:00Z'
};
const award = {
  id: 'a-1', campaign_id: 'c-1', draw_id: 'd-1', user_id: 42, ordinal: 1, prize_months: 3,
  interval_rule: 'after_current_term', state: 'needs_review', billing_state: 'uncertain', email_state: 'missing_contact',
  retry_count: 1, version: 2, selected_at: '2026-09-02T00:00:00Z', updated_at: '2026-09-02T00:00:00Z'
};

beforeEach(() => {
  admitted = true;
  reply = {};
  replies.clear();
  requests.length = 0;
  spies = stubDependencies();
});

afterEach(() => {
  for (const spy of spies) spy.mockRestore();
});

describe('giveaway RPC wire mapping', () => {
  it.each([
    { name: 'a candidate without a username falls back to the user id', candidates: [], login: '42' },
    { name: 'a candidate username becomes the winner login', candidates: [{ user_id: 42, username: 'live_winner', eligible: true }], login: 'live_winner' }
  ])('keeps the exact campaign and award fields without inventing dates: $name', async ({ candidates, login }) => {
    reply = { campaign, awards: [award], candidates };
    const detail = await giveawayGet({ actorId: 'admin-1', campaignId: 'c-1' });
    expect(detail).toMatchObject({ id: 'c-1', status: 'frozen', version: 4 });
    expect(detail.winners[0]).toMatchObject({ userId: '42', login, emailState: 'missing_contact', startAt: undefined, endAt: undefined });
  });

  it('preserves an explicit operation key and expected version for retries', async () => {
    await giveawayFreeze({ actorId: 'admin-1', campaignId: 'c-1', poolDigest: 'sha256:preview', idempotencyKey: 'c-1:freeze:4', expectedVersion: 4 });
    expect(requests.at(-1)).toEqual({
      subject: 'rpc.admin.giveaways.freeze',
      body: { actor_id: 'admin-1', idempotency_key: 'c-1:freeze:4', expected_version: 4, campaign_id: 'c-1', pool_digest: 'sha256:preview' }
    });
  });

  it('carries the authoritative preview digest into freeze and rejects a missing snapshot', () => {
    expect(freezePoolDigest({ poolDigest: 'sha256:preview' })).toBe('sha256:preview');
    expect(freezePoolDigest({ poolDigest: '  ' })).toBeNull();
  });

  it.each([
    ['banned', 'Banni'],
    ['inactive', 'Inactif'],
    ['not_onboarded', 'Configuration non terminée'],
    ['test_account', 'Compte de test'],
    ['vip', 'VIP'],
    ['current_staff', 'Équipe actuelle'],
    ['future_reason', 'Future Reason']
  ])('shows a readable label for %s', (reason, label) => {
    expect(giveawayExclusionLabel(reason, (key) => staticText('fr', key))).toBe(label);
  });

  it('maps the authoritative exclusion breakdown instead of counting eligible candidates', async () => {
    reply = {
      summary: {
        total: 10, eligible: 4, excluded: 6, free: 2, premium: 1, subscribers: 1,
        requested_winners: 1, prize_months: 3, total_prize_months: '3',
        exclusions: { banned: 1, inactive: 1, not_onboarded: 1, test_account: 1, vip: 1, current_staff: 1 }
      }, pool_digest: 'sha256:preview', capabilities: {
        new_awards_enabled: true, scheduling_enabled: false, provider_mutations_enabled: false, interval_rule_verified: false
      }
    };
    const preview = await giveawayPreview({ actorId: 'admin-1', winnerCount: 1, prizeMonths: 3 });
    expect(preview.exclusions).toEqual({ banned: 1, inactive: 1, not_onboarded: 1, test_account: 1, vip: 1, current_staff: 1 });
    expect(preview.poolDigest).toBe('sha256:preview');
  });

  it('adds an excluded account back and lists every exclusion with its reason', async () => {
    reply = {
      summary: {
        total: 3, eligible: 2, excluded: 1, included: 1, free: 1, premium: 1, subscribers: 0,
        requested_winners: 1, prize_months: 1, total_prize_months: '1',
        exclusions: { banned: 1, inactive: 0, not_onboarded: 0, test_account: 0, vip: 0, current_staff: 0 }
      },
      excluded: [
        { user_id: 2, username: 'two', eligible: true, exclusion_reason: 'vip', included_by: 10 },
        { user_id: 3, eligible: false, exclusion_reason: 'banned' }
      ],
      pool_digest: 'sha256:included', capabilities: {
        new_awards_enabled: true, scheduling_enabled: false, provider_mutations_enabled: false, interval_rule_verified: false
      }
    };
    const preview = await giveawayInclusion({ actorId: 'admin-1', campaignId: 'c-1', userId: '2', include: true });
    expect(requests.at(-1)).toMatchObject({ subject: 'rpc.admin.giveaways.include', body: { actor_id: 'admin-1', campaign_id: 'c-1', user_id: 2 } });
    expect(preview.included).toBe(1);
    expect(preview.excluded).toEqual([
      { userId: '2', login: 'two', reason: 'vip', included: true, includable: true },
      { userId: '3', login: '3', reason: 'banned', included: false, includable: false }
    ]);
    await giveawayInclusion({ actorId: 'admin-1', campaignId: 'c-1', userId: '2', include: false });
    expect(requests.at(-1)?.subject).toBe('rpc.admin.giveaways.exclude');
  });

  it.each([
    { algorithmVersion: 'crypto-rand-partial-fisher-yates-v1', selectionMethod: 'random_draw' },
    { algorithmVersion: 'fisher-yates-v1', selectionMethod: undefined },
    { algorithmVersion: 'unknown', selectionMethod: undefined }
  ])('keeps $algorithmVersion draw metadata separate from the selection method', async ({ algorithmVersion, selectionMethod }) => {
    reply = { campaign, draw: { pool_digest: 'sha256:draw', algorithm_version: algorithmVersion } };
    const detail = await giveawayGet({ actorId: admin.id, campaignId: campaign.id });
    expect(detail).toMatchObject({ poolDigest: 'sha256:draw', algorithmVersion, selectionMethod });
  });

  it.each([
    { status: 'frozen', candidates: [{ user_id: 41, eligible: true }, { user_id: 42, eligible: false }, { user_id: 43, eligible: true }], count: 2 },
    { status: 'draft', candidates: [{ user_id: 41, eligible: true }], count: undefined },
    { status: 'frozen', candidates: [], count: undefined }
  ])('projects $count eligible users for a $status giveaway', async ({ status, candidates, count }) => {
    reply = { campaign: { ...campaign, status }, candidates };
    const detail = await giveawayGet({ actorId: admin.id, campaignId: campaign.id });
    expect(detail.eligibleCount).toBe(count);
  });

  it('consumes the SvelteKit enhanced form result envelope', () => {
    const result = { type: 'success', data: { preview: { poolDigest: 'sha256:preview' } } };
    expect(actionPayload<typeof result.data>(result)?.preview.poolDigest).toBe('sha256:preview');
  });
});

const getSubject = 'rpc.admin.giveaways.get';
const alertsSubject = 'rpc.admin.giveaways.alerts';
const liveAlert = { id: 'incident', award_id: 'a-1', message: 'Payment pending', first_seen_at: '2026-09-03T00:00:00Z' };

async function loadDetail() {
  return await loadGiveaway({
    params: { id: campaign.id },
    parent: async () => admin,
    depends: () => {},
    url: new URL(`https://admin.example/giveaways/${campaign.id}`)
  } as never) as { giveaway: GiveawayDetailWire | null; alerts: Promise<GiveawayAlertWire[] | null> };
}

describe('giveaway detail load', () => {
  it('returns the campaign while the alerts RPC is pending', async () => {
    const pending = Promise.withResolvers<{ alerts: typeof liveAlert[] }>();
    replies.set(getSubject, { campaign, awards: [award] });
    replies.set(alertsSubject, pending.promise);
    const { giveaway, alerts } = await loadDetail();
    const started = requests.map(({ subject }) => subject).sort();
    pending.resolve({ alerts: [liveAlert] });
    expect({ started, campaign: giveaway?.id, alerts: await alerts }).toMatchObject({
      started: [alertsSubject, getSubject],
      campaign: campaign.id,
      alerts: [{ awardId: award.id }]
    });
  });

  it.each([
    { name: 'both reads answer', getFails: false, alertsFail: false, awardIds: ['a-1'] },
    { name: 'alerts fail', getFails: false, alertsFail: true, awardIds: null },
    { name: 'the giveaway fails', getFails: true, alertsFail: false, awardIds: null }
  ])('preserves the detail state when $name', async ({ getFails, alertsFail, awardIds }) => {
    replies.set(getSubject, getFails ? new Error('unavailable') : { campaign, awards: [award] });
    replies.set(alertsSubject, alertsFail ? new Error('unavailable') : {
      alerts: [liveAlert, { ...liveAlert, id: 'other-incident', award_id: 'a-2' }]
    });
    const { giveaway, alerts } = await loadDetail();
    expect({
      id: giveaway?.id,
      awardIds: (await alerts)?.map((alert) => alert.awardId) ?? null
    }).toEqual({ id: getFails ? undefined : campaign.id, awardIds: awardIds ? [...awardIds] : null });
  });
});

function submitTrial(action: keyof typeof trialActions, fields: { broadcaster_id: string; enabled?: string }, locale = 'fr') {
  const body = new FormData();
  for (const [key, value] of Object.entries(fields)) body.set(key, value);
  const request = new Request(`https://admin.example/trials?/${action}`, { method: 'POST', body });
  return trialActions[action]({ request, locals: { locale } } as never);
}

const trial = { broadcaster_id: '42' };

describe('trial action localization', () => {
  it.each([
    { action: 'add', fields: trial, locale: 'en', notice: 'Added 42. Waiting for Twitch chat subscription.' },
    { action: 'add', fields: trial, locale: 'fr', notice: 'Essai ajouté pour 42. Abonnement au clavardage Twitch en attente.' },
    { action: 'remove', fields: trial, locale: 'fr', notice: 'Arrêt de l’essai pour 42.' },
    { action: 'set_enabled', fields: { ...trial, enabled: 'true' }, locale: 'fr', notice: 'Essai activé pour 42.' },
    { action: 'set_enabled', fields: { ...trial, enabled: 'false' }, locale: 'fr', notice: 'Essai désactivé pour 42.' }
  ])('returns the $locale $action confirmation with the broadcaster ID', async ({ action, fields, locale, notice }) => {
    expect(await submitTrial(action, fields, locale)).toEqual({ ok: true, notice });
  });

  it.each([
    { action: 'add', fields: trial, admit: false, status: 403, error: 'accès interdit' },
    { action: 'remove', fields: { broadcaster_id: 'crumbs' }, admit: true, status: 400, error: 'Identifiant du diffuseur invalide.' },
    { action: 'set_enabled', fields: { ...trial, enabled: 'maybe' }, admit: true, status: 400, error: 'Valeur d’activation invalide.' }
  ])('localizes $action rejection with status $status before calling the service', async ({ action, fields, admit, status, error }) => {
    admitted = admit;
    expect(await submitTrial(action, fields)).toMatchObject({ status, data: { error } });
    expect(requests).toEqual([]);
  });

  it.each([
    { rpcError: 'full', error: 'Toutes les places d’essai sont prises.' },
    { rpcError: 'TIMEOUT', error: 'Les commandes d’essai sont indisponibles.' }
  ])('localizes the trial service error $rpcError', async ({ rpcError, error }) => {
    reply = new Error(rpcError);
    expect(await submitTrial('add', trial)).toMatchObject({ status: 400, data: { error } });
  });
});
