// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import type { VoiceAllowance } from '#lib/server/services/voice';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

const bus = await import('@bagel/kit/server/nats');
const { READ_TIMEOUT_MS, WRITE_TIMEOUT_MS } = await import('@bagel/kit/server/service');
const { SUB } = await import('#lib/server/services/subjects');
const { actions, load } = await import('./+page.server');

const owner = { user_id: '42' };
const delegate = { user_id: '95', delegate_of: '42', sections: ['modules'] };
const viewAs = { user_id: '42', impersonator_id: '97' };
const actor = { actor_id: '42', broadcaster_user_id: '42' };
const allowance: VoiceAllowance = {
  plan: 'premium', period: '2026-11', limit_tokens: 90_000, used_tokens: 20_025,
  remaining_tokens: 69_975, resets_at: '2026-12-01T00:00:00Z', enabled: true, voice: 'Puck'
};
const free: VoiceAllowance = {
  ...allowance, plan: 'free', limit_tokens: 0, used_tokens: 0, remaining_tokens: 0, enabled: false, voice: 'Kore'
};
const voiceUrl = 'https://dashboard.test/voice';

afterEach(() => mock.restore());

test.each([
  { name: 'Premium owner', session: owner, reply: { ...allowance, reason: 'ok' }, readOnly: false },
  { name: 'free owner', session: owner, reply: { ...free, reason: 'no_plan' }, readOnly: false },
  { name: 'inactive owner', session: owner, reply: { ...free, reason: 'inactive' }, readOnly: false },
  { name: 'banned owner', session: owner, reply: { ...free, reason: 'banned' }, readOnly: false },
  { name: 'owner with a reply without a reason', session: owner, reply: free, readOnly: false },
  { name: 'delegate', session: delegate, reply: null, readOnly: true },
  { name: 'view-as', session: viewAs, reply: null, readOnly: true }
])('loads voice data for $name', async ({ session, reply, readOnly }) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(reply);
  expect(await load({ locals: { session } } as never)).toEqual({ readOnly, allowance: reply });
  expect(rpc.mock.calls).toEqual(readOnly ? [] : [[`${SUB.dashboard}.voice_get`, actor, READ_TIMEOUT_MS]]);
});

test('an unavailable allowance degrades without inventing a free plan', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await load({ locals: { session: owner } } as never)).toEqual({ readOnly: false, allowance: null });
});

function save(fields: Record<string, string>, session: typeof owner | null = owner) {
  const body = new URLSearchParams(fields);
  return actions.save({
    request: new Request(`${voiceUrl}?/save`, { method: 'POST', body }),
    locals: { session, locale: 'en' }
  } as never);
}

test.each([
  { voice: 'Puck', enabled: true },
  { voice: 'Kore', enabled: false }
])('saves $voice with enabled=$enabled using the owner session', async ({ voice, enabled }) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ ok: true });
  const fields = { voice, enabled: String(enabled), actor_id: '97', broadcaster_user_id: '95' };
  expect(await save(fields)).toEqual({ ok: true });
  expect(rpc.mock.calls).toEqual([[
    `${SUB.dashboard}.voice_set`, { ...actor, voice, enabled }, WRITE_TIMEOUT_MS
  ]]);
});

test.each([
  { voice: 'kore', enabled: 'true', session: owner, status: 400 },
  { voice: '', enabled: 'true', session: owner, status: 400 },
  { voice: 'Kore', enabled: 'yes', session: owner, status: 400 },
  { voice: 'Kore', enabled: 'true', session: delegate, status: 403 },
  { voice: 'Kore', enabled: 'true', session: viewAs, status: 403 },
  { voice: 'Kore', enabled: 'true', session: null, status: 401 }
])('rejects invalid or unauthorized saves with $status', async ({ voice, enabled, session, status }) => {
  const rpc = spyOn(bus, 'rpc');
  expect(await save({ voice, enabled }, session)).toMatchObject({ status, data: { error: expect.any(String) } });
  expect(rpc).not.toHaveBeenCalled();
});

test('reports a failed save without claiming it succeeded', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await save({ voice: 'Kore', enabled: 'false' })).toMatchObject({
    status: 503, data: { error: 'Could not save voice settings. Try again.' }
  });
});
