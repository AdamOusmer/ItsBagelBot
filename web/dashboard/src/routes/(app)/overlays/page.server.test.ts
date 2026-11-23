// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { POWERED_ORIGIN, defaultPoweredConfig } from '@bagel/kit/overlays';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: () => {},
  scheduleLeaderboardPurge: () => {}
}));

const bus = await import('@bagel/kit/server/nats');
const { READ_TIMEOUT_MS, WRITE_TIMEOUT_MS } = await import('@bagel/kit/server/service');
const { actions, load } = await import('./+page.server');

const owner = { user_id: '42' };
const delegate = { user_id: '95', delegate_of: '42', sections: ['modules'] };
const pageUrl = 'https://dashboard.test/overlays';
const config = { ...defaultPoweredConfig(), every_minutes: 30, directions: ['left'], align: 'center' };
const system = { id: 7, kind: 'system', template: 'powered', name: 'Powered by Bagel', revision: 9 };
const listReply = { overlays: [system] };
const getReply = { ...system, document: { config } };
const form = { id: '7', every_minutes: '30', show_seconds: '8', directions: 'left', align: 'center', expected_rev: '9' };
const loaded = { readOnly: false, previewOrigin: POWERED_ORIGIN, overlays: [system], powered: { id: 7, config, revision: 9 } };

afterEach(() => mock.restore());

type Action = 'save' | 'add' | 'rotate' | 'delete';

function post(action: Action, session: object | null, fields: Record<string, string> = {}) {
  return actions[action]({
    request: new Request(`${pageUrl}?/${action}`, { method: 'POST', body: new URLSearchParams(fields) }),
    locals: { session, locale: 'en' },
    setHeaders: () => {}
  } as never);
}

test('loads the list and the powered settings for an owner', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(listReply).mockResolvedValueOnce(getReply);
  expect(await load({ locals: { session: owner, locale: 'en' } } as never)).toEqual(loaded);
  expect(rpc.mock.calls).toEqual([
    ['rpc.overlays.overlay.list', { user_id: '42' }, READ_TIMEOUT_MS],
    ['rpc.overlays.overlay.get', { user_id: '42', id: 7 }, READ_TIMEOUT_MS]
  ]);
});

test.each([
  { name: 'no powered overlay yet', replies: [{ overlays: [] }], want: { ...loaded, overlays: [], powered: null }, calls: 1 },
  { name: 'a failed list', replies: [new Error('timeout')], want: { ...loaded, overlays: null, powered: null }, calls: 1 },
  { name: 'a failed settings read', replies: [listReply, new Error('timeout')], want: { ...loaded, powered: null }, calls: 2 }
])('loads for an owner with $name', async ({ replies, want, calls }) => {
  const rpc = spyOn(bus, 'rpc');
  for (const reply of replies) {
    if (reply instanceof Error) rpc.mockRejectedValueOnce(reply);
    else rpc.mockResolvedValueOnce(reply);
  }
  expect(await load({ locals: { session: owner, locale: 'en' } } as never)).toEqual(want);
  expect(rpc).toHaveBeenCalledTimes(calls);
});

test('a delegate gets a read-only page without calling the owner', async () => {
  const rpc = spyOn(bus, 'rpc');
  expect(await load({ locals: { session: delegate, locale: 'en' } } as never)).toEqual({
    readOnly: true, previewOrigin: POWERED_ORIGIN, overlays: null, powered: null
  });
  expect(rpc).not.toHaveBeenCalled();
});

test('saves the whole config with the expected revision', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ ...getReply, revision: 10 });
  expect(await post('save', owner, form)).toEqual({ ok: true, rev: 10 });
  expect(rpc.mock.calls).toEqual([['rpc.overlays.overlay.save', { user_id: '42', id: 7, config, revision: 9 }, WRITE_TIMEOUT_MS]]);
});

test.each([
  { name: 'a wait under the floor', fields: { every_minutes: '4' }, error: 'Choose from 5 to 120 minutes.' },
  { name: 'a wait over the cap', fields: { every_minutes: '121' }, error: 'Choose from 5 to 120 minutes.' },
  { name: 'a show over the cap', fields: { show_seconds: '21' }, error: 'Choose from 6 to 20 seconds.' },
  { name: 'no edges', fields: { directions: '' }, error: 'Pick an edge.' },
  { name: 'two edges', fields: { directions: 'top,left' }, error: 'Pick an edge.' },
  { name: 'an unknown edge', fields: { directions: 'middle' }, error: 'Pick an edge.' },
  { name: 'an unknown position', fields: { align: 'middle' }, error: 'Choose a position on the edge.' }
])('refuses $name before calling the owner', async ({ fields, error }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await post('save', owner, { ...form, ...fields })).toMatchObject({ status: 400, data: { error } });
  expect(rpc).not.toHaveBeenCalled();
});

test.each([
  { code: 'conflict', want: { status: 409, data: { conflict: true } } },
  { code: 'invalid', want: { status: 400, data: { error: expect.any(String) } } },
  { code: 'internal', want: { status: 503, data: { error: expect.any(String) } } }
])('maps a $code refusal from the owner', async ({ code, want }) => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new bus.RpcError('refused', code));
  expect(await post('save', owner, form)).toMatchObject(want);
});

const linkCases: { action: Action; fields: Record<string, string>; subject: string; request: object }[] = [
  { action: 'add', fields: {}, subject: 'add_template', request: { user_id: '42', template: 'powered' } },
  { action: 'rotate', fields: { id: '7' }, subject: 'rotate', request: { user_id: '42', id: 7 } }
];

test.each(linkCases)('$action returns the id, the one-time link and the revision', async ({ action, fields, subject, request }) => {
  const url = 'https://overlay.itsbagelbot.com/o#BBBB';
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ ...system, url, revision: 10 });
  expect(await post(action, owner, fields)).toEqual({ id: 7, url, revision: 10 });
  expect(rpc.mock.calls).toEqual([[`rpc.overlays.overlay.${subject}`, request, WRITE_TIMEOUT_MS]]);
});

test('deleting asks the owner to drop the overlay', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({});
  expect(await post('delete', owner, { id: '7' })).toEqual({ deleted: true });
  expect(rpc.mock.calls).toEqual([['rpc.overlays.overlay.delete', { user_id: '42', id: 7 }, WRITE_TIMEOUT_MS]]);
});

test.each([
  { name: 'no session', session: null, status: 401 },
  { name: 'a delegate', session: delegate, status: 403 }
])('$name is refused on every action without calling the owner', async ({ session, status }) => {
  const rpc = spyOn(bus, 'rpc');
  for (const action of ['save', 'add', 'rotate', 'delete'] as const) {
    expect(await post(action, session, form)).toMatchObject({ status });
  }
  expect(rpc).not.toHaveBeenCalled();
});

test.each(['add', 'rotate', 'delete'] as const)('a failed %s says so', async (action) => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await post(action, owner, form)).toMatchObject({
    status: 503, data: { listError: 'Could not update your overlays. Try again.' }
  });
});
