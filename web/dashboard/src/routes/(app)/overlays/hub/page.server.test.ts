// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { POWERED_ORIGIN, defaultPoweredConfig } from '@bagel/kit/overlays';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();
mock.module('#lib/server/alpha', () => ({ alphaOnly: (handler: unknown) => handler, alphaActions: (actions: unknown) => actions }));
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

afterEach(() => mock.restore());

type Action = 'add' | 'rotate' | 'delete';

function post(action: Action, session: object | null, fields: Record<string, string> = {}) {
  return actions[action]({
    request: new Request(`${pageUrl}?/${action}`, { method: 'POST', body: new URLSearchParams(fields) }),
    locals: { session, locale: 'en' },
    setHeaders: () => {}
  } as never);
}

test.each([
  { name: 'the list', replies: [listReply], want: { readOnly: false, overlays: [system] } },
  { name: 'a failed list', replies: [new Error('timeout')], want: { readOnly: false, overlays: null } }
])('loads $name for an owner without reading the powered settings', async ({ replies, want }) => {
  const rpc = spyOn(bus, 'rpc');
  for (const reply of replies) {
    if (reply instanceof Error) rpc.mockRejectedValueOnce(reply);
    else rpc.mockResolvedValueOnce(reply);
  }
  expect(await load({ locals: { session: owner, locale: 'en' } } as never)).toEqual(want);
  expect(rpc.mock.calls).toEqual([['rpc.overlays.overlay.list', { user_id: '42' }, READ_TIMEOUT_MS]]);
});

test('a delegate gets a read-only page without calling the owner', async () => {
  const rpc = spyOn(bus, 'rpc');
  expect(await load({ locals: { session: delegate, locale: 'en' } } as never)).toEqual({ readOnly: true, overlays: null });
  expect(rpc).not.toHaveBeenCalled();
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
  const refusals = await Promise.all((['add', 'rotate', 'delete'] as const).map((action) => post(action, session, { id: '7' })));
  expect(refusals.map((refusal) => (refusal as { status: number }).status)).toEqual([status, status, status]);
  expect(rpc).not.toHaveBeenCalled();
});

test.each(['add', 'rotate', 'delete'] as const)('a failed %s says so', async (action) => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await post(action, owner, { id: '7' })).toMatchObject({
    status: 503, data: { listError: 'Could not update your overlays. Try again.' }
  });
});
