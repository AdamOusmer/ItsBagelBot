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
const view = { url: 'https://overlay.itsbagelbot.com/powered#AAAA', config, revision: 9 };
const form = { every_minutes: '30', show_seconds: '8', directions: 'left', align: 'center', expected_rev: '9' };

afterEach(() => mock.restore());

function post(action: 'save' | 'rotate', session: object | null, fields: Record<string, string> = {}) {
  return actions[action]({
    request: new Request(`${pageUrl}?/${action}`, { method: 'POST', body: new URLSearchParams(fields) }),
    locals: { session, locale: 'en' },
    setHeaders: () => {}
  } as never);
}

test.each([
  { name: 'an owner', session: owner, read: () => Promise.resolve(view), want: { readOnly: false, previewOrigin: POWERED_ORIGIN, powered: view }, calls: 1 },
  { name: 'an owner whose read fails', session: owner, read: () => Promise.reject(new Error('timeout')), want: { readOnly: false, previewOrigin: POWERED_ORIGIN, powered: null }, calls: 1 },
  { name: 'a delegate', session: delegate, read: () => Promise.resolve(view), want: { readOnly: true, previewOrigin: POWERED_ORIGIN, powered: null }, calls: 0 }
])('loads the powered badge for $name', async ({ session, read, want, calls }) => {
  const rpc = spyOn(bus, 'rpc').mockImplementationOnce(read as never);
  expect(await load({ locals: { session, locale: 'en' } } as never)).toEqual(want);
  expect(rpc.mock.calls).toEqual(Array(calls).fill(['rpc.overlays.powered.get', { user_id: '42' }, READ_TIMEOUT_MS]));
});

test('saves the whole config with the expected revision', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ ...view, revision: 10 });
  expect(await post('save', owner, form)).toEqual({ ok: true, rev: 10 });
  expect(rpc.mock.calls).toEqual([['rpc.overlays.powered.save', { user_id: '42', config, revision: 9 }, WRITE_TIMEOUT_MS]]);
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

test('replacing the link returns the new link and revision', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ ...view, url: 'https://overlay.itsbagelbot.com/powered#BBBB', revision: 10 });
  expect(await post('rotate', owner)).toEqual({ url: 'https://overlay.itsbagelbot.com/powered#BBBB', revision: 10 });
  expect(rpc.mock.calls).toEqual([['rpc.overlays.powered.rotate', { user_id: '42' }, WRITE_TIMEOUT_MS]]);
});

test.each([
  { name: 'no session', action: 'save', session: null, want: { status: 401, data: { error: expect.any(String) } } },
  { name: 'a delegate', action: 'save', session: delegate, want: { status: 403, data: { error: expect.any(String) } } },
  { name: 'no session', action: 'rotate', session: null, want: { status: 401, data: { rotateError: expect.any(String) } } },
  { name: 'a delegate', action: 'rotate', session: delegate, want: { status: 403, data: { rotateError: expect.any(String) } } }
] as const)('$action refuses $name without calling the owner', async ({ action, session, want }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await post(action, session, form)).toMatchObject(want);
  expect(rpc).not.toHaveBeenCalled();
});

test('a failed replace keeps the old link and says so', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await post('rotate', owner)).toMatchObject({ status: 503, data: { rotateError: 'Could not replace the link. Try again.' } });
});
