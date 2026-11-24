// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { registerServerConfig } from '@bagel/kit/server/config';
import { stubSvelteKit } from './sveltekit';

stubSvelteKit();
registerServerConfig({ cacheInvalidationPrefix: 'inv' });
mock.module('virtual:bagel-i18n', () => ({ urls: {}, loaders: {}, sliced: false }));
mock.module('@bagel/kit/i18n', () => ({ translate: (_locale: string, key: string) => key }));

const RANK = { moderator: 1, admin: 2, owner: 3 } as const;
const NEEDS = { 'campaigns.manage': 'admin', 'campaigns.approve': 'owner' } as const;

let role: keyof typeof RANK = 'owner';
const identity = () => ({ id: '7', login: 'staff', display_name: 'Staff', role });
const allows = (actor: keyof typeof RANK, key: keyof typeof NEEDS) => RANK[actor] >= RANK[NEEDS[key]];
mock.module('#lib/server/access', () => ({
  allows,
  requireRole: async (_event: unknown, key: keyof typeof NEEDS) => (allows(role, key) ? identity() : null)
}));
const audit = mock(() => {});
mock.module('#lib/server/audit', () => ({ audit }));

type Call = { subject: string; request: Record<string, unknown> };
let calls: Call[] = [];
let reply: Record<string, unknown> | Error = {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  publish: async () => {},
  rpc: async (subject: string, request: Record<string, unknown>) => {
    calls.push({ subject, request });
    if (reply instanceof Error) throw reply;
    return reply;
  }
}));

const list = await import('../src/routes/(admin)/campaigns/+page.server');
const detail = await import('../src/routes/(admin)/campaigns/[id]/+page.server');

const campaign = { id: 4, name: 'Launch', revision: 3, status: 'draft', content: { en: { subject: 's', html: '<p>h</p>', text: 't' } }, sends: {} };

beforeEach(() => {
  role = 'owner';
  calls = [];
  reply = { campaign };
  audit.mockClear();
});

function post(path: string, fields: Record<string, string>) {
  const body = new FormData();
  for (const [name, value] of Object.entries(fields)) body.set(name, value);
  return { request: new Request(`https://admin.test${path}`, { method: 'POST', body }), locals: { locale: 'en' }, params: { id: '4' } } as never;
}

const act = (name: keyof typeof detail.actions, fields: Record<string, string> = {}) =>
  (detail.actions[name] as (event: never) => Promise<unknown>)(post(`/campaigns/4?/${name}`, fields));

describe('campaign list', () => {
  test('streams the campaigns, and marks the page degraded when the service is down', async () => {
    reply = { campaigns: [campaign] };
    const ok = (await list.load({ parent: async () => ({ role: 'admin', id: '7' }), depends: () => {} } as never)) as { history: Promise<unknown> };
    expect(await ok.history).toEqual({ campaigns: [campaign], degraded: false });
    expect(calls[0]).toEqual({ subject: 'rpc.admin.marketing.campaign_list', request: { actor_id: '7' } });

    reply = new Error('TIMEOUT');
    const down = (await list.load({ parent: async () => ({ role: 'admin', id: '7' }), depends: () => {} } as never)) as { history: Promise<unknown> };
    expect(await down.history).toEqual({ campaigns: [], degraded: true });
  });

  test('moderators are sent home', async () => {
    await expect(list.load({ parent: async () => ({ role: 'moderator', id: '7' }), depends: () => {} } as never)).rejects.toMatchObject({ status: 302 });
  });

  test('creating a draft saves an empty campaign and returns its id', async () => {
    const result = await (list.actions.create as (e: never) => Promise<unknown>)(post('/campaigns?/create', { name: '  Launch  ' }));
    expect(result).toMatchObject({ action: { ok: true }, campaign: { id: 4 } });
    expect(calls[0]).toEqual({ subject: 'rpc.admin.marketing.campaign_save', request: { actor_id: '7', name: 'Launch', content: {} } });
  });

  test('a blank name never reaches the service', async () => {
    const result = await (list.actions.create as (e: never) => Promise<unknown>)(post('/campaigns?/create', { name: '   ' }));
    expect(result).toMatchObject({ status: 400 });
    expect(calls).toEqual([]);
  });
});

describe('campaign editor load', () => {
  test('returns the campaign, the audience and whether this account may approve', async () => {
    reply = { campaign, eligible: { en: 3, fr: 1 }, eligible_total: 4 };
    const data = (await detail.load({ params: { id: '4' }, parent: async () => ({ role: 'admin', id: '7' }), depends: () => {} } as never)) as {
      campaign: unknown; audience: Promise<unknown>; canApprove: boolean;
    };
    expect(data).toMatchObject({ campaign, canApprove: false });
    expect(await data.audience).toEqual({ eligible: { en: 3, fr: 1 }, total: 4 });
  });

  test('a service failure leaves the campaign empty for the unavailable banner', async () => {
    reply = new Error('TIMEOUT');
    const data = (await detail.load({ params: { id: '4' }, parent: async () => ({ role: 'owner', id: '7' }), depends: () => {} } as never)) as { campaign: unknown };
    expect(data.campaign).toBeNull();
  });

  test('a non numeric id is a 404', async () => {
    await expect(detail.load({ params: { id: 'abc' }, parent: async () => ({ role: 'owner', id: '7' }), depends: () => {} } as never)).rejects.toMatchObject({ status: 404 });
  });
});

describe('campaign actions', () => {
  const content = JSON.stringify({ en: { subject: 's', html: '<p>h</p>', text: 't' } });

  test('save sends the parsed draft', async () => {
    expect(await act('save', { name: 'Launch', content })).toMatchObject({ action: { ok: true, notice: 'admin.campaigns.saved' } });
    expect(calls[0]).toEqual({
      subject: 'rpc.admin.marketing.campaign_save',
      request: { actor_id: '7', id: 4, name: 'Launch', content: JSON.parse(content) }
    });
  });

  test.each([
    { name: 'unknown locale', content: JSON.stringify({ xx: { subject: '', html: '', text: '' } }) },
    { name: 'malformed JSON', content: '{' },
    { name: 'a part that is not text', content: JSON.stringify({ en: { subject: 1, html: '', text: '' } }) },
    { name: 'an oversized subject', content: JSON.stringify({ en: { subject: 'x'.repeat(201), html: '', text: '' } }) }
  ])('save refuses $name before calling the service', async ({ content }) => {
    expect(await act('save', { name: 'Launch', content })).toMatchObject({ status: 400 });
    expect(calls).toEqual([]);
  });

  test('the test email goes to the signed-in staff account', async () => {
    reply = {};
    expect(await act('test')).toMatchObject({ action: { ok: true, notice: 'admin.campaigns.testSent' } });
    expect(calls[0]).toEqual({ subject: 'rpc.admin.marketing.campaign_test', request: { actor_id: '7', id: 4 } });
  });

  test.each([
    { code: 'unavailable', notice: 'admin.campaigns.senderUnavailable' },
    { code: 'conflict', notice: 'admin.campaigns.stateConflict' },
    { code: 'invalid', notice: 'admin.campaigns.invalidCampaign' }
  ] as const)('a $code refusal shows a translated notice', async ({ code, notice }) => {
    reply = new nats.RpcError('raw service text', code);
    expect(await act('test')).toEqual({ action: { ok: false, notice } });
  });

  test('only owners approve, and the refusal never reaches the service', async () => {
    role = 'admin';
    expect(await act('approve', { revision: '3' })).toMatchObject({ status: 403 });
    expect(calls).toEqual([]);
    role = 'owner';
    expect(await act('approve', { revision: '3' })).toMatchObject({ action: { ok: true, notice: 'admin.campaigns.approved' } });
    expect(calls[0].subject).toBe('rpc.admin.marketing.campaign_approve');
  });

  test('approval carries the revision the owner reviewed', async () => {
    await act('approve', { revision: '3' });
    expect(calls[0].request).toEqual({ actor_id: '7', id: 4, revision: 3 });
  });

  test.each([
    { name: 'missing', fields: {} as Record<string, string> },
    { name: 'not a number', fields: { revision: 'x' } },
    { name: 'zero', fields: { revision: '0' } }
  ])(
    'approval refuses a $name revision before calling the service',
    async ({ fields }) => {
      expect(await act('approve', fields)).toMatchObject({ status: 400 });
      expect(calls).toEqual([]);
    }
  );

  test('a stale revision shows the conflict notice', async () => {
    reply = new nats.RpcError('raw service text', 'conflict');
    expect(await act('approve', { revision: '2' })).toEqual({ action: { ok: false, notice: 'admin.campaigns.stateConflict' } });
  });

  test.each([
    { name: 'pause', verb: 'campaign_pause', notice: 'admin.campaigns.paused' },
    { name: 'resume', verb: 'campaign_resume', notice: 'admin.campaigns.resumed' }
  ] as const)('$name is audited and reaches $verb', async ({ name, verb, notice }) => {
    role = 'admin';
    expect(await act(name)).toMatchObject({ action: { ok: true, notice } });
    expect(calls[0].subject).toBe(`rpc.admin.marketing.${verb}`);
    expect(audit).toHaveBeenCalledTimes(1);
  });

  test('moderators cannot run any action', async () => {
    role = 'moderator';
    for (const name of ['save', 'test', 'approve', 'pause', 'resume'] as const) {
      expect(await act(name, { name: 'x', content })).toMatchObject({ status: 403 });
    }
    expect(calls).toEqual([]);
  });
});
