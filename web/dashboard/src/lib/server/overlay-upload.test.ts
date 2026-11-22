// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, expect, mock, spyOn, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const bus = await import('@bagel/kit/server/nats');
const { WRITE_TIMEOUT_MS } = await import('@bagel/kit/server/service');
const { beginUpload, relayPart } = await import('./overlay-upload');

const owner = { user_id: '42' };
const delegate = { user_id: '95', delegate_of: '42', sections: ['modules'] };
const ticket = { ticket: 'T1T1T1T1T1T1T1T1.sig', max_size: 3, expires: 1, part_size: 33554432, parts: 1 };
const url = (query: string) => new URL(`https://dashboard.test/overlays/assets?${query}`);

beforeEach(() => {
  privateEnv.OVERLAYS_INTERNAL_URL = 'https://overlays.test:8081';
  privateEnv.OVERLAYS_INTERNAL_CA_PEM = 'pem';
});
afterEach(() => {
  delete privateEnv.OVERLAYS_INTERNAL_URL;
  delete privateEnv.OVERLAYS_INTERNAL_CA_PEM;
  mock.restore();
});

const begin = (kind: 'assets' | 'imports', session: object | null, body: unknown) =>
  beginUpload(kind, session as never, new Request('https://dashboard.test/overlays/assets', { method: 'POST', body: JSON.stringify(body) }));

function part(query: string, session: object | null = owner, body: Blob | null = new Blob([new Uint8Array([1, 2, 3])])) {
  const headers = { 'content-length': String(body?.size ?? 0) };
  return relayPart('assets', session as never, new Request(url(query), { method: 'PUT', body, headers }), url(query));
}

test('begin asks the overlay service for a ticket and returns the part plan', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(ticket);

  const response = await begin('assets', owner, { declared_size: 3, name: 'logo.png' });

  expect([response.status, await response.json()]).toEqual([200, ticket]);
  expect(rpc.mock.calls).toEqual([
    ['rpc.overlays.assets.begin', { user_id: '42', display_name: 'logo.png', declared_size: 3 }, WRITE_TIMEOUT_MS]
  ]);
});

test('a free account can begin a dry-run import', async () => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(ticket);

  expect((await begin('imports', owner, { declared_size: 3, dry_run: true })).status).toBe(200);
  expect(rpc.mock.calls[0].slice(0, 2)).toEqual(['rpc.overlays.imports.begin', { user_id: '42', declared_size: 3, dry_run: true }]);
});

test.each([
  { name: 'nobody signed in', session: null, rejection: null, status: 401, error: 'not_signed_in' },
  { name: 'a delegate', session: delegate, rejection: null, status: 403, error: 'owner_only' },
  { name: 'a free account', session: owner, rejection: new bus.RpcError('premium_required', 'forbidden'), status: 403, error: 'premium_required' },
  { name: 'a full account', session: owner, rejection: new bus.RpcError('quota_exceeded', 'forbidden'), status: 403, error: 'quota_exceeded' },
  { name: 'too many uploads', session: owner, rejection: new bus.RpcError('rate_limited', 'unavailable'), status: 429, error: 'rate_limited' },
  { name: 'a bus outage', session: owner, rejection: new Error('timeout'), status: 503, error: 'unavailable' }
])('begin refuses $name', async ({ session, rejection, status, error }) => {
  const rpc = spyOn(bus, 'rpc');
  if (rejection) rpc.mockRejectedValueOnce(rejection);

  const response = await begin('assets', session, { declared_size: 3 });

  expect([response.status, await response.json()]).toEqual([status, { error }]);
});

test('a part is forwarded unchanged to the overlay service', async () => {
  const upstream = spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json({ part: 2 }));

  const response = await part('ticket=T1T1T1T1T1T1T1T1.sig&part=2');

  expect([response.status, await response.json()]).toEqual([200, { part: 2 }]);
  expect(upstream.mock.calls[0][0]).toBe('https://overlays.test:8081/assets/T1T1T1T1T1T1T1T1.sig?part=2');
  expect(upstream.mock.calls[0][1]).toMatchObject({ method: 'PUT', headers: { 'content-length': '3' }, tls: { ca: 'pem' } });
});

test('finishing forwards the done flag and a dry-run request', async () => {
  const upstream = spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json({ report: {} }));

  expect((await part('ticket=T1T1T1T1T1T1T1T1.sig&done=1&dry_run=1', owner, null)).status).toBe(200);
  expect(upstream.mock.calls[0][0]).toBe('https://overlays.test:8081/assets/T1T1T1T1T1T1T1T1.sig?done=1&dry_run=1');
});

test.each([
  { name: 'nobody signed in', query: 'ticket=T1T1T1T1T1T1T1T1.sig&part=1', session: null, status: 401 },
  { name: 'a delegate', query: 'ticket=T1T1T1T1T1T1T1T1.sig&part=1', session: delegate, status: 403 },
  { name: 'a ticket that tries to leave the path', query: 'ticket=..%2F..%2Fx&part=1', session: owner, status: 400 },
  { name: 'a part number that is not a number', query: 'ticket=T1T1T1T1T1T1T1T1.sig&part=1x', session: owner, status: 400 },
  { name: 'neither part nor done', query: 'ticket=T1T1T1T1T1T1T1T1.sig', session: owner, status: 400 }
])('refuses $name before anything leaves the dashboard', async ({ query, session, status }) => {
  const upstream = spyOn(globalThis, 'fetch');

  expect((await part(query, session)).status).toBe(status);
  expect(upstream).not.toHaveBeenCalled();
});

test('passes the overlay service refusal through unchanged', async () => {
  spyOn(globalThis, 'fetch').mockResolvedValueOnce(Response.json({ error: 'too_large' }, { status: 413 }));

  const response = await part('ticket=T1T1T1T1T1T1T1T1.sig&done=1', owner, null);

  expect([response.status, await response.json()]).toEqual([413, { error: 'too_large' }]);
});
