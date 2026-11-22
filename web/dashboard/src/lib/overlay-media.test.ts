// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { MediaFailure } from '@bagel/kit/overlay-editor';
import { ASSET_ENDPOINT, dashboardMedia } from './overlay-media';

const uploaded = { asset_id: 'abc', url: 'https://cdn.itsbagelbot.com/u/1/abc', type: 'png', w: 10, h: 20, duration_ms: 0, bytes: 5 };
const file = new File([new Uint8Array(5)], 'logo ü.png', { type: 'image/png' });

function reply(status: number, body: unknown): typeof fetch {
  return (async () => Response.json(body, { status })) as unknown as typeof fetch;
}

test('streams the file to the asset endpoint and returns the contract reply', async () => {
  const calls: [string, RequestInit][] = [];
  const send = (async (url: string, init: RequestInit) => {
    calls.push([url, init]);
    return Response.json(uploaded);
  }) as unknown as typeof fetch;
  const result = await dashboardMedia(send).upload(file);
  const [url, init] = calls[0]!;
  expect([result, url, init.method, init.body === file, init.headers]).toEqual([
    uploaded, ASSET_ENDPOINT, 'POST', true, { 'content-type': 'image/png', 'x-asset-name': encodeURIComponent('logo ü.png') }
  ]);
});

test.each([
  { name: 'a coded refusal', status: 400, body: { code: 'asset_dimensions' }, code: 'asset_dimensions' },
  { name: 'a premium status', status: 402, body: {}, code: 'premium_required' },
  { name: 'an oversized status', status: 413, body: {}, code: 'asset_too_large' },
  { name: 'a rate limit status', status: 429, body: {}, code: 'rate_limited' },
  { name: 'an unknown status', status: 500, body: {}, code: 'failed' }
])('maps $name to $code', async ({ status, body, code }) => {
  const failure = await dashboardMedia(reply(status, body)).upload(file).catch((error: unknown) => error);
  expect([failure instanceof MediaFailure, (failure as MediaFailure).code]).toEqual([true, code]);
});
