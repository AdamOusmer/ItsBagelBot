// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { ImportFailure } from '@bagel/kit/overlay-editor';
import { IMPORT_ENDPOINT, importArchive } from './overlay-import';

const report = { mapped: [{ subject: 'a.png' }], approximated: [], unsupported: [{ subject: 'b.mp4', reason: 'convert_to_webm' }] };
const zip = new File([new Uint8Array(4)], 'pack.zip', { type: 'application/zip' });

function recorder(response: Response) {
  const calls: [string, RequestInit][] = [];
  const send = (async (url: string, init: RequestInit) => {
    calls.push([url, init]);
    return response;
  }) as unknown as typeof fetch;
  return { calls, send };
}

test.each([
  { name: 'a dry run', dryRun: true, url: `${IMPORT_ENDPOINT}?dry_run=1` },
  { name: 'a confirmed import', dryRun: false, url: IMPORT_ENDPOINT }
])('posts the archive for $name', async ({ dryRun, url }) => {
  const { calls, send } = recorder(Response.json({ report, id: 9 }));
  const result = await importArchive(zip, dryRun, send);
  const [target, init] = calls[0]!;
  expect([result, target, init.method, init.body === zip, init.headers]).toEqual([
    { report, id: 9 }, url, 'POST', true, { 'content-type': 'application/zip' }
  ]);
});

test.each([
  { name: 'a coded refusal', status: 400, body: { code: 'bomb_ratio' }, code: 'bomb_ratio' },
  { name: 'a premium status', status: 402, body: {}, code: 'premium_required' },
  { name: 'an unknown code', status: 400, body: { code: 'odd' }, code: 'failed' },
  { name: 'an unreadable body', status: 500, body: {}, code: 'failed' }
])('maps $name to $code', async ({ status, body, code }) => {
  const { send } = recorder(Response.json(body, { status }));
  const failure = await importArchive(zip, true, send).catch((error: unknown) => error);
  expect([failure instanceof ImportFailure, (failure as ImportFailure).code]).toEqual([true, code]);
});
