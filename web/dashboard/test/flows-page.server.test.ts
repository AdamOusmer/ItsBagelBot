// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, test } from 'bun:test';
import { formRequest, installAutomationsRpcFake } from './automations-rpc';
import { privateEnv, stubSvelteKit } from './sveltekit';

stubSvelteKit({ dev: true });
await import('../src/lib/server/action-errors');
const fake = await installAutomationsRpcFake();
const { actions } = await import('../src/routes/(app)/flows/[id]/+page.server');

privateEnv.DEMO = '1';
const demoModule = '../src/routes/(app)/flows/[id]/+page.server.ts?demo';
const { actions: demoActions } = (await import(demoModule)) as typeof import('../src/routes/(app)/flows/[id]/+page.server');
delete privateEnv.DEMO;

const RUN = 'rpc.automations.run';
const name = 'Scene change';
const session = { user_id: '7' };
const delegate = { user_id: '9', delegate_of: '7', sections: ['modules'] };

beforeEach(() => fake.reset());

function post(run: typeof actions.run, fields = { name }, status = 'paid', actor = session) {
  const request = formRequest('https://dashboard.itsbagelbot.com/flows/a-recipe?/run', fields);
  const locals = { session: actor, locale: 'en', accountState: { value: { status } } };
  return run({ request, locals } as never);
}

test('live run uses the saved name and the delegated broadcaster ID', async () => {
  fake.replies['rpc.dashboard.state_get'] = { status: 'paid' };
  fake.replies[RUN] = { accepted: true, run_id: 'run-1' };
  expect(await post(actions.run, { name }, 'paid', delegate)).toEqual({ ok: true });
  expect(fake.calls.filter((call) => call.subject === RUN)).toEqual([
    { subject: RUN, body: { broadcaster_id: '7', name, source: 'dashboard', request_id: expect.any(String) } }
  ]);
});

test.each([
  ['invalid', { accepted: false, code: 'invalid' }, 400],
  ['forbidden', { accepted: false, code: 'forbidden' }, 403],
  ['not_found', { accepted: false, code: 'not_found' }, 404],
  ['unavailable', { accepted: false, code: 'unavailable' }, 502],
  ['failed', new Error('Sesame timed out'), 502]
] as const)('live run maps %s to an action failure', async (code, reply, status) => {
  fake.replies[RUN] = reply;
  expect(await post(actions.run)).toMatchObject({ status, data: { ok: false, code } });
});

test('demo run succeeds without RPC', async () => {
  expect({ reply: await post(demoActions.run), calls: fake.calls }).toEqual({ reply: { ok: true }, calls: [] });
});

test('run denies free accounts at the save gate', async () => {
  expect(await post(actions.run, { name }, 'free')).toMatchObject({
    status: 403,
    data: { ok: false, code: 'forbidden', error: expect.stringContaining('Premium') }
  });
  expect(fake.calls).toEqual([]);
});

test('run requires a saved recipe name', async () => {
  expect(await post(actions.run, { name: ' ' })).toMatchObject({ status: 400, data: { ok: false, code: 'invalid' } });
  expect(fake.calls).toEqual([]);
});
