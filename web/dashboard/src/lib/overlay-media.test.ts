// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { MediaFailure } from '@bagel/kit/overlay-editor';
import { ASSET_ENDPOINT, dashboardMedia } from './overlay-media';
import { fakeUpload } from './overlay-upload-fake';

const uploaded = {
  asset_id: 'abc', url: 'https://cdn.itsbagelbot.com/u/1/abc', type: 'png', w: 10, h: 20, duration_ms: 0, bytes: 5, sha256: 'a'.repeat(64)
};
const file = new File([new Uint8Array(5)], 'logo ü.png', { type: 'image/png' });

test('opens a ticket, sends each part, then completes and returns the asset view', async () => {
  const { calls, send } = fakeUpload(Response.json(uploaded));
  const result = await dashboardMedia(send).upload(file);
  const [open, ...rest] = calls;
  const ticket = `${ASSET_ENDPOINT}?ticket=tkt-0123456789abcdef`;
  expect([result, JSON.parse(String(open!.body)), rest.map((call) => [call.method, call.url])]).toEqual([
    uploaded,
    { declared_size: 5, name: 'logo ü.png' },
    [['PUT', `${ticket}&part=1`], ['PUT', `${ticket}&part=2`], ['PUT', `${ticket}&done=1`]]
  ]);
});

test.each([
  { name: 'a coded refusal', status: 422, body: { error: 'asset_dimensions' }, code: 'asset_dimensions' },
  { name: 'a forbidden status', status: 403, body: {}, code: 'alpha_required' },
  { name: 'an oversized status', status: 413, body: {}, code: 'asset_too_large' },
  { name: 'a rate limit status', status: 429, body: {}, code: 'rate_limited' },
  { name: 'an unknown status', status: 500, body: {}, code: 'failed' }
])('maps $name to $code', async ({ status, body, code }) => {
  const { send } = fakeUpload(Response.json(body, { status }));
  const failure = await dashboardMedia(send).upload(file).catch((error: unknown) => error);
  expect([failure instanceof MediaFailure, (failure as MediaFailure).code]).toEqual([true, code]);
});
