// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { registerServerConfig } from '@bagel/kit/server/config';

process.env.NEW_RELIC_ENABLED = 'false';
registerServerConfig({ cacheInvalidationPrefix: 'inv' });

let deliver: (subject: string, body: Uint8Array) => void = () => {};
let gap = () => {};
let failed = false;
const calls: string[] = [];
const published: unknown[] = [];
const user = { id: 42, username: 'oldname', is_active: true, status: 'free', banned: false };
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string) => {
    calls.push(subject);
    if (failed) throw new Error('write failed');
    return { user, users: [user], admins: [], entries: [], role: 'viewer' };
  },
  publish: async (subject: string, payload: unknown) => { published.push({ subject, payload }); },
  subscribeDurable: (_subject: string, message: typeof deliver, onGap: typeof gap) => {
    deliver = message;
    gap = onGap;
  }
}));
const services = await import('./services');
services.startInvalidationListener();

function invalidation(scope: string, id = '42', keys: string[] = []): void {
  deliver(`inv.${scope}`, new TextEncoder().encode(JSON.stringify({ broadcaster_id: id, keys })));
}

beforeEach(() => {
  gap();
  calls.length = 0;
  published.length = 0;
  failed = false;
});

test('rename drops both login aliases and the account snapshot', async () => {
  for (const login of ['oldname', 'newname', 'unrelated']) await services.userLookup('1', login);
  invalidation('rename', '42', ['OldName', 'NewName']);
  calls.length = 0;
  for (const login of ['oldname', 'newname', 'unrelated']) await services.userLookup('1', login);
  expect(calls).toEqual(['rpc.admin.user.get', 'rpc.admin.user.get']);
});

test.each(['automations', 'loyalty', 'outgress-pause', 'fetches'])('%s preserves unrelated admin user and token caches', async (scope) => {
  const read = () => Promise.all([services.userList('1'), services.userLookup('1', '42'), services.tokenStatus({ actorId: '1', userId: '42' })]);
  await read();
  calls.length = 0;
  invalidation(scope);
  await read();
  expect(calls).toEqual([]);
});

test.each(['status', 'user'])('%s drops only the affected authorization entry', async (scope) => {
  await services.adminCheck('42');
  await services.adminCheck('7');
  calls.length = 0;
  invalidation(scope);
  await services.adminCheck('42');
  await services.adminCheck('7');
  expect(calls).toEqual(['rpc.admin.user.auth.check']);
});

test.each(['userBan', 'userSetStatus'] as const)('%s drops authorization locally after its durable write', async (method) => {
  await services.adminCheck('42');
  await services[method]({ actorId: '1', userId: '42' }, 'paid');
  calls.length = 0;
  await services.adminCheck('42');
  expect(calls).toEqual(['rpc.admin.user.auth.check']);
});

test.each([false, true])('audit append broadcasts only after success, failure=%s', async (writeFailed) => {
  await services.auditList();
  failed = writeFailed;
  await services.auditAppend({ actor_id: '42', actor_login: 'actor', action: 'test', ok: true }).catch(() => undefined);
  failed = false;
  calls.length = 0;
  await services.auditList();
  expect([calls, published]).toEqual(writeFailed ? [[], []] : [
    ['rpc.admin.user.audit.list'], [{ subject: 'inv.audit', payload: { broadcaster_id: '42' } }]
  ]);
});

test.each(['audit', 'staff'])('%s self-broadcast evicts another pod cache', async (scope) => {
  const read = () => Promise.all([services.auditList(), services.adminListAccts(), services.adminCheck('42')]);
  await read();
  calls.length = 0;
  invalidation(scope);
  await read();
  expect(calls.sort()).toEqual(scope === 'audit' ? ['rpc.admin.user.audit.list'] : ['rpc.admin.user.auth.check', 'rpc.admin.user.auth.list']);
});
