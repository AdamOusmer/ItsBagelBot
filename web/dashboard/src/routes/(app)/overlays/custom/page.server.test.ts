// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: () => {},
  scheduleLeaderboardPurge: () => {}
}));

const bus = await import('@bagel/kit/server/nats');
const { READ_TIMEOUT_MS, WRITE_TIMEOUT_MS } = await import('@bagel/kit/server/service');
const { createOverlay } = await import('@bagel/kit/overlay-editor');
const { actions, load } = await import('./+page.server');

const owner = { user_id: '42' };
const delegate = { user_id: '95', delegate_of: '42', sections: ['modules'] };
const document = createOverlay('alerts', 'Alerts', (kind) => kind);
const stored = { id: 8, kind: 'custom', name: 'Alerts', revision: 3, document };

afterEach(() => mock.restore());

function visit(session: object | null, query: string, isPremium: boolean) {
  return load({
    locals: { session, locale: 'en' },
    url: new URL(`https://dashboard.test/overlays/custom${query}`),
    parent: async () => ({ isPremium })
  } as never);
}

function post(session: object | null, fields: Record<string, string>) {
  return actions.save({
    request: new Request('https://dashboard.test/overlays/custom?/save', { method: 'POST', body: new URLSearchParams(fields) }),
    locals: { session, locale: 'en' }
  } as never);
}

test('a premium owner opening an overlay gets its document', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(stored);
  const data = await visit(owner, '?id=8', true);
  expect([data, rpc.mock.calls]).toEqual([
    { readOnly: false, preview: false, overlay: { id: 8, name: 'Alerts', revision: 3, document }, missing: false },
    [['rpc.overlays.overlay.get', { user_id: '42', id: 8 }, READ_TIMEOUT_MS]]
  ]);
});

test.each([
  { name: 'a system overlay', premium: true, reply: { ...stored, kind: 'system' }, calls: 1, missing: true },
  { name: 'a failed read', premium: true, reply: new Error('timeout'), calls: 1, missing: true },
  { name: 'a free account', premium: false, reply: stored, calls: 0, missing: false }
])('opening $name shows no document', async ({ premium, reply, calls, missing }) => {
  const rpc = spyOn(bus, 'rpc');
  if (reply instanceof Error) rpc.mockRejectedValueOnce(reply);
  else rpc.mockResolvedValueOnce(reply);
  expect(await visit(owner, '?id=8', premium)).toMatchObject({ overlay: null, missing });
  expect(rpc).toHaveBeenCalledTimes(calls);
});

test.each([
  { name: 'a new overlay', query: '', want: { preview: false } },
  { name: 'the free preview', query: '?preview=1', want: { preview: true } }
])('$name loads without calling the owner', async ({ query, want }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await visit(owner, query, false)).toEqual({ readOnly: false, overlay: null, missing: false, ...want });
  expect(rpc).not.toHaveBeenCalled();
});

test('a delegate gets a read-only page and a signed-out visitor is sent to log in', async () => {
  const rpc = spyOn(bus, 'rpc');
  const readOnly = await visit(delegate, '?id=8', true);
  const signedOut = await visit(null, '', true).catch((redirect: { status: number }) => redirect.status);
  expect([readOnly, signedOut, rpc.mock.calls.length]).toEqual([{ readOnly: true, preview: false, overlay: null, missing: false }, 302, 0]);
});

test('saving sends the validated document as a custom overlay with its revision', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ ...stored, revision: 1 });
  const reply = await post(owner, { id: '0', document: JSON.stringify(document) });
  const [subject, payload, timeout] = rpc.mock.calls[0]!;
  expect([reply, subject, timeout, payload]).toEqual([
    { ok: true, id: 8, revision: 1 },
    'rpc.overlays.overlay.save',
    WRITE_TIMEOUT_MS,
    expect.objectContaining({ user_id: '42', kind: 'custom', name: 'Alerts', revision: 0 })
  ]);
});

test('saving an existing overlay names it in the request', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(stored);
  await post(owner, { id: '8', document: JSON.stringify({ ...document, rev: 3 }) });
  expect(rpc.mock.calls[0]![1]).toMatchObject({ id: 8, revision: 3 });
});

test.each([
  { code: 'conflict', status: 409 },
  { code: 'invalid', status: 400 },
  { code: 'forbidden', status: 402 },
  { code: 'internal', status: 503 }
])('maps a $code refusal to $status', async ({ code, status }) => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new bus.RpcError('refused', code as never));
  expect(await post(owner, { id: '8', document: JSON.stringify(document) })).toMatchObject({ status, data: { error: expect.any(String) } });
});

test.each([
  { name: 'broken JSON', document: '{' },
  { name: 'a document that fails validation', document: JSON.stringify({ ...document, widgets: [{ id: 'x' }] }) },
  { name: 'no document', document: '' }
])('refuses $name before calling the owner', async ({ document: body }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await post(owner, { id: '8', document: body })).toMatchObject({ status: 400 });
  expect(rpc).not.toHaveBeenCalled();
});

test.each([
  { name: 'no session', session: null, status: 401 },
  { name: 'a delegate', session: delegate, status: 403 }
])('$name cannot save', async ({ session, status }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await post(session, { id: '8', document: JSON.stringify(document) })).toMatchObject({ status });
  expect(rpc).not.toHaveBeenCalled();
});
