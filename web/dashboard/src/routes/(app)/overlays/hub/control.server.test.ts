// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();
mock.module('#lib/server/alpha', () => ({ alphaOnly: (handler: unknown) => handler, alphaActions: (actions: unknown) => actions }));
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: () => {},
  scheduleLeaderboardPurge: () => {}
}));

const bus = await import('@bagel/kit/server/nats');
const { READ_TIMEOUT_MS, WRITE_TIMEOUT_MS } = await import('@bagel/kit/server/service');
const assets = await import('../assets/+server');
const feed = await import('../feed/+server');

const owner = { user_id: '42' };
const delegate = { user_id: '95', delegate_of: '42', sections: ['modules'] };

afterEach(() => mock.restore());

const control = (body: unknown) =>
  new Request('https://dashboard.test/overlays/feed', { method: 'POST', body: JSON.stringify(body) });

test('listing assets asks the owner for its own assets', async () => {
  const asset = { asset_id: 'a1', url: 'https://cdn.test/a1.png', type: 'image', bytes: 1, w: 1, h: 1, duration_ms: 0, sha256: 'x' };
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ assets: [asset] });
  const response = await assets.GET({ locals: { session: owner } } as never);
  expect([await response.json(), rpc.mock.calls]).toEqual([
    { assets: [asset] },
    [['rpc.overlays.assets.list', { user_id: '42' }, READ_TIMEOUT_MS]]
  ]);
});

test.each([
  { name: 'a good id', query: '?id=a1', status: 200, calls: 1 },
  { name: 'a malformed id', query: '?id=../x', status: 400, calls: 0 },
  { name: 'no id', query: '', status: 400, calls: 0 }
])('deleting an asset with $name', async ({ query, status, calls }) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValue({});
  const response = await assets.DELETE({ locals: { session: owner }, url: new URL(`https://dashboard.test/overlays/assets${query}`) } as never);
  expect([response.status, rpc.mock.calls.length]).toEqual([status, calls]);
});

test('a feed control names the owner channel and the acting user', async () => {
  const state = { control: { paused: true, muted: false, skip: 0 }, subathon: { running: false } };
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(state);
  const response = await feed.POST({ locals: { session: owner }, request: control({ action: 'pause', args: {} }) } as never);
  expect([await response.json(), rpc.mock.calls]).toEqual([
    state,
    [['rpc.overlays.feed.control', { user_id: '42', actor: '42', action: 'pause', args: {} }, WRITE_TIMEOUT_MS]]
  ]);
});

test.each([
  { name: 'an unknown action', session: owner, body: { action: 'explode' }, status: 400 },
  { name: 'array args', session: owner, body: { action: 'test', args: [] }, status: 400 },
  { name: 'a delegate', session: delegate, body: { action: 'pause' }, status: 403 },
  { name: 'no session', session: null, body: { action: 'pause' }, status: 401 }
])('refuses $name before calling the owner', async ({ session, body, status }) => {
  const rpc = spyOn(bus, 'rpc');
  const response = await feed.POST({ locals: { session }, request: control(body) } as never);
  expect([response.status, rpc.mock.calls.length]).toEqual([status, 0]);
});

test('a server refusal keeps its status', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new bus.RpcError('unavailable', 'unavailable'));
  const response = await feed.POST({ locals: { session: owner }, request: control({ action: 'test', args: { kind: 'follow' } }) } as never);
  expect(response.status).toBe(503);
});
