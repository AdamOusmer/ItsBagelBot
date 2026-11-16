// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';
import { actionEvent, captureAnalytics } from '../../../../../test/analytics';

stubSvelteKit();
const { IMPORT_SOURCES } = await import('@bagel/kit');
let allowed = true;
let fails = false;
let invalid = false;
let inputRefused = false;
let credentialMissing = false;
const calls: unknown[] = [];
mock.module('@bagel/kit/server/rate-limit', () => ({
  ValkeyRateLimiter: class { async check() { return { allowed }; } }
}));
const importer = await import('../../../../lib/server/importer');
const strategies = Object.fromEntries(IMPORT_SOURCES.map((id) => [id, {
  id,
  acceptInput: () => inputRefused ? { status: 400, error: 'Choose a file to upload.' } : null,
  credential: () => credentialMissing ? null : 'private-token'
}]));
mock.module('#lib/server/importer', () => ({
  ...importer,
  SERVER_STRATEGIES: strategies,
  previewImport: async (...args: unknown[]) => {
    calls.push(args);
    if (fails) throw new Error('unavailable');
    return { manifest: invalid ? null : { commands: [] } };
  },
  commitImport: async (...args: unknown[]) => {
    calls.push(args);
    if (fails) throw new Error('unavailable');
    return { error: invalid ? 'Nothing selected to import.' : '' };
  }
}));
const { actions } = await import('./+page.server');
const recorded = captureAnalytics();
beforeEach(() => {
  allowed = true;
  fails = false;
  invalid = false;
  inputRefused = false;
  credentialMissing = false;
  calls.length = 0;
});

test.each([
  { action: 'preview', result: 'preview_ok', status: undefined },
  { action: 'preview', fails: true, result: 'preview_failed', status: 502 },
  { action: 'preview', invalid: true, result: 'preview_failed', status: 422 },
  { action: 'preview', input: true, result: 'preview_failed', status: 400 },
  { action: 'preview', credential: true, result: 'preview_failed', status: 400 },
  { action: 'preview', allowed: false, result: 'preview_failed', status: 429 },
  { action: 'commit', result: 'committed', status: undefined },
  { action: 'commit', fails: true, result: 'commit_failed', status: 502 },
  { action: 'commit', invalid: true, result: 'commit_failed', status: 422 },
  { action: 'commit', manifest: '', result: 'commit_failed', status: 400 },
  { action: 'commit', manifest: '{broken', result: 'commit_failed', status: 400 },
  { action: 'commit', allowed: false, result: 'commit_failed', status: 429 }
])('$action reports $result ($status)', async (row) => {
  allowed = row.allowed ?? true;
  fails = row.fails ?? false;
  invalid = row.invalid ?? false;
  inputRefused = row.input ?? false;
  credentialMissing = row.credential ?? false;
  const event = actionEvent({ source: 'streamelements', credential: 'private-token', manifest: row.manifest ?? '{"commands":[]}' });
  const response = await actions[row.action](event as never);
  expect({ status: (response as { status?: number }).status, counts: await recorded() }).toEqual({
    status: row.status, counts: [[`import.runs|streamelements|${row.result}`, 1]]
  });
});

test.each(['preview', 'commit'])('%s drops unknown source text from analytics', async (action) => {
  await actions[action](actionEvent({ source: 'private-login', manifest: '{}' }) as never);
  expect(await recorded()).toEqual([]);
});

test.each(IMPORT_SOURCES)('counts committed imports from %s', async (source) => {
  await actions.commit(actionEvent({ source, manifest: '{}' }) as never);
  expect(await recorded()).toEqual([[`import.runs|${source}|committed`, 1]]);
});
