// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { ImportFailure } from '@bagel/kit/overlay-editor';
import { IMPORT_ENDPOINT, importArchive } from './overlay-import';
import { fakeUpload } from './overlay-upload-fake';

const report = { mapped: [{ subject: 'a.png' }], approximated: [], unsupported: [{ subject: 'b.mp4', reason: 'convert_to_webm' }] };
const zip = new File([new Uint8Array(4)], 'pack.zip', { type: 'application/zip' });

test.each([
  { name: 'a dry run', dryRun: true, done: `${IMPORT_ENDPOINT}?ticket=tkt-0123456789abcdef&done=1&dry_run=1` },
  { name: 'a confirmed import', dryRun: false, done: `${IMPORT_ENDPOINT}?ticket=tkt-0123456789abcdef&done=1` }
])('uploads the archive in parts for $name', async ({ dryRun, done }) => {
  const { calls, send } = fakeUpload(Response.json({ report, overlay_id: 9 }));
  const result = await importArchive(zip, dryRun, send);
  expect([result, JSON.parse(String(calls[0]!.body)), calls.at(-1)!.url]).toEqual([
    { report, overlay_id: 9 }, { declared_size: 4, dry_run: dryRun }, done
  ]);
});

test.each([
  { name: 'a coded refusal', status: 422, body: { error: 'bomb_ratio' }, code: 'bomb_ratio' },
  { name: 'a forbidden status', status: 403, body: {}, code: 'alpha_required' },
  { name: 'an unknown code', status: 400, body: { error: 'odd' }, code: 'failed' },
  { name: 'an unreadable body', status: 500, body: {}, code: 'failed' }
])('maps $name to $code', async ({ status, body, code }) => {
  const { send } = fakeUpload(Response.json(body, { status }));
  const failure = await importArchive(zip, true, send).catch((error: unknown) => error);
  expect([failure instanceof ImportFailure, (failure as ImportFailure).code]).toEqual([true, code]);
});
