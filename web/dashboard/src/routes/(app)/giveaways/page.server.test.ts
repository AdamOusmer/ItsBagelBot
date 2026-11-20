// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

const bus = await import('@bagel/kit/server/nats');
const moduleStore = await import('#lib/server/commands-store');
const { SUB } = await import('#lib/server/services/subjects');
const { actions, load } = await import('./+page.server');

const round_id = '32ff6551-4646-4fb4-bf55-02268d943b1a';
const session = { user_id: '95', delegate_of: '42', sections: ['modules'] };
const bystander = { ...session, sections: ['commands'] };

afterEach(() => mock.restore());

function invoke(action: keyof typeof actions, fields: Record<string, string | undefined>, caller: typeof session | null = session) {
  const body = new FormData();
  for (const [name, value] of Object.entries(fields)) {
    if (value !== undefined) body.set(name, value);
  }
  return actions[action]({
    request: new Request(`https://dashboard.test/giveaways?/${action}`, { method: 'POST', body }),
    locals: { locale: 'en', session: caller }
  } as never);
}

test.each([
  { action: 'start', verb: 'start', input: { preset: 'regulars', prize: 'A game', winners: 3, window_seconds: 90 } },
  { action: 'close', verb: 'close', input: { round_id } },
  { action: 'extend', verb: 'extend', input: { round_id } },
  { action: 'cancel', verb: 'cancel', input: { round_id } },
  { action: 'reroll', verb: 'reroll', input: { round_id, slot: 0 } },
  { action: 'end', verb: 'end_slots', input: { round_id, slot: 0 } },
  { action: 'skip', verb: 'skip_rule', input: { round_id, slot: 4 } },
  { action: 'deliver', verb: 'mark_delivered', input: { round_id, slot: 1, delivered: false } }
])('delegated $action uses the broadcaster and actual moderator', async ({ action, verb, input }) => {
  const rpc = spyOn(bus, 'rpcReply').mockResolvedValueOnce({ done: true });
  const fields = Object.fromEntries(Object.entries(input).map(([key, value]) => [key, String(value)]));
  expect(await invoke(action, fields)).toEqual({ ok: true });
  expect(rpc).toHaveBeenCalledWith(`rpc.draw.giveaway.${verb}`,
    { user_id: '42', source: 'dashboard', actor_id: '95', ...input }, 10_000);
});

test.each([
  { action: 'start', fields: { winners: '101' }, caller: session, status: 400 },
  { action: 'skip', fields: { round_id, slot: '0' }, caller: session, status: 400 },
  { action: 'close', fields: { round_id: 'bad' }, caller: session, status: 400 },
  { action: 'deliver', fields: { round_id, slot: '1', delivered: 'nah' }, caller: session, status: 400 },
  { action: 'close', fields: { round_id }, caller: bystander, status: 403 },
  { action: 'start', fields: {}, caller: null, status: 401 }
])('rejects $action with status $status before RPC', async ({ action, fields, caller, status }) => {
  const rpc = spyOn(bus, 'rpcReply');
  expect(await invoke(action, fields, caller)).toMatchObject({ status, data: { ok: false } });
  expect(rpc).not.toHaveBeenCalled();
});

test.each([
  { code: 'conflict', error: 'stale', status: 409, copy: 'The giveaway changed. Review the latest state and try again.' },
  { code: 'conflict', error: 'busy', reason: 'giveaway.open.busy', status: 409, copy: 'A raffle is already open. Draw or cancel it in chat first.' },
  { code: 'forbidden', error: 'refused', reason: 'giveaway.prize_blocked', status: 403, copy: 'Choose a different prize name.' }
])('localizes a draw refusal with code $code', async ({ status, copy, ...reply }) => {
  spyOn(bus, 'rpcReply').mockResolvedValueOnce(reply);
  expect(await invoke('start', { prize: 'A game' })).toMatchObject({ status, data: { ok: false, error: copy } });
});

test.each([
  { claim: '0', expected: { claimSeconds: 0 } },
  { claim: '120', expected: { claimSeconds: 120 } },
  { claim: '', expected: { claimSeconds: 120 } }
])('saves claim duration $claim as a typed module patch', async ({ claim, expected }) => {
  const patch = spyOn(moduleStore, 'patchModule').mockResolvedValueOnce({ rev: 8, conflict: false, synced: true });
  const fields = { expected_rev: '7', is_enabled: 'on', key: 'claimSeconds', value: claim };
  expect(await invoke('saveSettings', fields)).toEqual({ ok: true });
  expect(patch).toHaveBeenCalledWith({
    userId: '42', name: 'giveaway', isEnabled: true, expectedRev: 7, partial: expected
  });
});

test.each([
  { names: ['Regulars'], value: 'Regulars', saved: true },
  { names: ['Regulars', 'regulars'], value: 'Regulars', saved: false },
  { names: ['Regulars'], value: 'missing', saved: false }
])('validates preset names $names and default $value', async ({ names, value, saved }) => {
  const patch = spyOn(moduleStore, 'patchModule').mockResolvedValueOnce({ rev: 8, conflict: false, synced: true });
  const rules = { min_watch_seconds: 7200, sub_luck_percent: 200, skip_winners_days: 3 };
  const presets = JSON.stringify(names.map((name) => ({ name, rules, freezeWhileOpen: false })));
  const fields = { expected_rev: '7', is_enabled: 'on', presets, default_preset: value };
  const result = await invoke('savePresets', fields);
  expect(result).toMatchObject(saved ? { ok: true } : { status: 400 });
  expect(patch.mock.calls.map(([call]) => call.partial)).toEqual(saved ? [{
    defaultPreset: 'regulars', presets: [{ name: 'regulars', rules }]
  }] : []);
});

test.each([false, true])('loads settings even when live is degraded ($s)', async (degraded) => {
  const modules = spyOn(moduleStore, 'listModulesForEditing').mockResolvedValueOnce([
    { name: 'giveaway', is_enabled: false, revision: 7, configs: { defaultPrize: 'A game', refusalEnabled: false } }
  ]);
  const rpc = spyOn(bus, 'rpc').mockImplementation(async (subject) => {
    if (subject.endsWith('.recent')) return { winners: [{ round_id, slot: 1, prize: 'A game' }] } as never;
    if (degraded) throw new Error('timeout');
    return { rounds: [] } as never;
  });
  const depends = mock(() => {});
  const data = await load({ locals: { session }, depends, url: new URL('https://dashboard.test/giveaways?winners=2') } as never);
  expect(data).toMatchObject({
    enabled: false, revision: 7, config: { fields: { defaultPrize: 'A game', refusalEnabled: 'off' } },
    live: { rounds: [], degraded }, history: { page: 2, winners: [{ round_id, slot: 1 }] }
  });
  expect([modules.mock.calls, depends.mock.calls]).toEqual([[['42']], [['giveaways:live']]]);
  expect(rpc.mock.calls).toEqual([
    ['rpc.draw.giveaway.live', { user_id: '42', source: 'dashboard' }, 10_000],
    [`${SUB.modules}.giveaway.recent`, { user_id: '42', limit: 21, offset: 20 }, 2000]
  ]);
});
