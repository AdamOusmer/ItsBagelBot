// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, test } from 'bun:test';
import { formRequest, installAutomationsRpcFake } from '../../../../../test/automations-rpc';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();
await import('../../../../lib/server/action-errors');
const fake = await installAutomationsRpcFake();
const { actions, load } = await import('./+page.server');

const MOD = 'bagel.rpc.modules.automation';
const RUNS = 'bagel.rpc.automations.runs';
const manual = { version: 1, trigger: { type: 'manual', params: {} }, actions: [] };
const stored = { id: 'a1', user_id: 7, name: 'Hello', enabled: true, revision: 4, definition: manual };
const other = { ...stored, id: 'b2', name: 'Other' };
const session = { user_id: '7' };

beforeEach(() => fake.reset());

function read(id: string) {
  return load({ locals: { session }, params: { id } } as never) as Promise<Record<string, unknown>>;
}

function save(fields: Record<string, string>, status = 'paid') {
  const request = formRequest('https://dashboard.itsbagelbot.com/automations/a1', fields);
  return actions.save({ request, locals: { session, locale: 'en', accountState: { value: { status } } } } as never);
}

const draft = (over: Record<string, string> = {}) => ({
  id: 'a1',
  name: 'Hello',
  enabled: 'on',
  revision: '4',
  definition: JSON.stringify(manual),
  ...over
});

describe('automation editor load', () => {
  test('new starts blank, lists the other automations and skips the run log', async () => {
    fake.replies[`${MOD}.list`] = { automations: [stored] };
    expect(await read('new')).toEqual({
      id: 'new',
      enabled: true,
      automation: null,
      missing: false,
      runs: [],
      others: [{ id: 'a1', name: 'Hello' }]
    });
    expect(fake.calls.map((c) => c.subject)).toEqual([`${MOD}.list`]);
  });

  test('an existing automation loads with its last runs and without itself as a run target', async () => {
    fake.replies[`${MOD}.list`] = { automations: [stored, other] };
    fake.replies[`${MOD}.get`] = { automation: stored };
    fake.replies[RUNS] = { runs: [{ run_id: 'r1' }] };
    const data = await read('a1');
    expect(data).toMatchObject({ automation: stored, missing: false, runs: [{ run_id: 'r1' }], others: [{ id: 'b2', name: 'Other' }] });
    expect(fake.calls.find((c) => c.subject === RUNS)?.body).toEqual({ broadcaster_id: '7', automation_id: 'a1', limit: 50 });
  });

  test('an unknown id is flagged missing', async () => {
    fake.replies[`${MOD}.list`] = { automations: [] };
    fake.replies[`${MOD}.get`] = { code: 'not_found' };
    fake.replies[RUNS] = { runs: [] };
    expect(await read('zzz')).toMatchObject({ automation: null, missing: true });
  });

  test('a failing run log does not block the editor', async () => {
    fake.replies[`${MOD}.list`] = { automations: [stored] };
    fake.replies[`${MOD}.get`] = { automation: stored };
    fake.replies[RUNS] = new Error('sesame down');
    expect(await read('a1')).toMatchObject({ automation: stored, runs: [] });
  });
});

describe('automation editor save', () => {
  test('create returns the new id', async () => {
    fake.replies[`${MOD}.upsert`] = { automation: { ...stored, id: 'fresh', revision: 1 } };
    expect(await save(draft({ id: '', revision: '' }))).toEqual({ ok: true, id: 'fresh' });
    expect(fake.calls[0].body).toMatchObject({ user_id: '7', name: 'Hello', enabled: true, definition: manual });
    expect(fake.calls[0].body.id).toBeUndefined();
    expect(fake.calls[0].body.expected_revision).toBeUndefined();
  });

  test('update sends the revision guard', async () => {
    fake.replies[`${MOD}.upsert`] = { automation: { ...stored, revision: 5 } };
    expect(await save(draft())).toEqual({ ok: true, id: 'a1' });
    expect(fake.calls[0].body).toMatchObject({ id: 'a1', expected_revision: 4 });
  });

  test('owner issues come back by path for the form to place', async () => {
    const issues = [{ path: 'actions[0].params.text', code: 'required' }];
    fake.replies[`${MOD}.upsert`] = { code: 'invalid', issues };
    expect(await save(draft())).toMatchObject({ status: 400, data: { ok: false, code: 'invalid', issues, conflict: false } });
  });

  test('a duplicate name is a field issue', async () => {
    fake.replies[`${MOD}.upsert`] = { code: 'conflict', issues: [{ path: 'name', code: 'duplicate' }] };
    expect(await save(draft())).toMatchObject({ status: 400, data: { issues: [{ path: 'name', code: 'duplicate' }], conflict: false } });
  });

  test('a stale revision asks for a reload', async () => {
    fake.replies[`${MOD}.upsert`] = { conflict: true };
    expect(await save(draft())).toMatchObject({ status: 409, data: { code: 'conflict', conflict: true } });
  });

  test('a definition that is not JSON never reaches the owner', async () => {
    expect(await save(draft({ definition: '{nope' }))).toMatchObject({ status: 400, data: { ok: false } });
    expect(fake.calls).toEqual([]);
  });
});

describe('the automation table holds only real automations', () => {
  test.each([
    ['a command with one chat line', { version: 1, trigger: { type: 'command', params: { name: '!hi' } }, actions: [{ type: 'chat', params: { text: 'hi' } }] }],
    ['a bare follow line', { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, actions: [{ type: 'chat', params: { text: 'hi' } }] }]
  ])('the form editor refuses %s', async (_label, definition) => {
    const res = await save({ name: 'Hello', definition: JSON.stringify(definition) });
    expect(res).toMatchObject({ status: 400, data: { code: 'belongs_elsewhere' } });
    expect(fake.calls).toEqual([]);
  });
});

describe('premium beta', () => {
  test('save refuses a free account before reaching a service', async () => {
    const res = await save({ name: 'Hello', definition: JSON.stringify({ version: 1, trigger: { type: 'manual' } }) }, 'free');
    expect(res).toMatchObject({ status: 403, data: { error: expect.stringContaining('Premium') } });
    expect(fake.calls).toEqual([]);
  });
});

describe('automation editor run', () => {
  test('runs by name through Sesame', async () => {
    fake.replies['bagel.rpc.automations.run'] = { accepted: true, run_id: 'r1' };
    const request = formRequest('https://dashboard.itsbagelbot.com/automations/a1', { name: 'Hello' });
    const locals = { session, locale: 'en', accountState: { value: { status: 'paid' } } };
    expect(await actions.run({ request, locals } as never)).toEqual({ ok: true });
  });
});
