// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, test } from 'bun:test';
import { formRequest, installAutomationsRpcFake } from '../../../../test/automations-rpc';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
await import('../../../lib/server/action-errors');
const fake = await installAutomationsRpcFake();
const { actions, load } = await import('./+page.server');

const MOD = 'bagel.rpc.modules.automation';
const manual = { version: 1, trigger: { type: 'manual', params: {} }, actions: [] };
const stored = { id: 'a1', user_id: 7, name: 'Hello', enabled: true, revision: 4, definition: manual };
const session = { user_id: '7' };

beforeEach(() => fake.reset());

function post(name: keyof typeof actions, fields: Record<string, string>, who: unknown = session, status = 'paid') {
  const request = formRequest('https://dashboard.itsbagelbot.com/automations', fields);
  const locals = { session: who, locale: 'en', accountState: { value: { status } } };
  return (actions[name] as (e: never) => Promise<unknown>)({ request, locals } as never);
}

describe('automations list load', () => {
  test('returns the module switch with the channel automations', async () => {
    fake.replies[`${MOD}.list`] = { automations: [stored] };
    const data = (await load({ locals: { session } } as never)) as { enabled: boolean; automations: unknown[] };
    expect(data).toEqual({ enabled: true, automations: [stored] });
    expect(fake.calls[0].body).toEqual({ user_id: '7' });
  });

  test('a disabled module is reported so the page can offer the switch', async () => {
    fake.moduleEnabled = false;
    fake.replies[`${MOD}.list`] = { automations: [] };
    expect(await load({ locals: { session } } as never)).toEqual({ enabled: false, automations: [] });
  });

  test('an unreachable owner degrades the page', async () => {
    fake.replies[`${MOD}.list`] = new Error('down');
    expect(await load({ locals: { session } } as never)).toMatchObject({ degraded: true, automations: [] });
  });

  test('a delegate without the modules section is sent home', async () => {
    const delegate = { user_id: '9', delegate_of: '7', sections: ['commands'] };
    await expect(load({ locals: { session: delegate } } as never)).rejects.toMatchObject({ status: 302 });
  });
});

describe('automations list actions', () => {
  test('toggle switches the module blob', async () => {
    expect(await post('toggle', { is_enabled: 'on' })).toEqual({ ok: true });
    expect(fake.moduleWrites).toEqual([true]);
  });

  test('setEnabled sends only the flag and the shown revision', async () => {
    fake.replies[`${MOD}.set_enabled`] = { automation: { ...stored, enabled: false, revision: 5 } };
    expect(await post('setEnabled', { id: 'a1', enabled: '', revision: '4' })).toEqual({ ok: true });
    expect(fake.calls[0].body).toEqual({ user_id: '7', id: 'a1', enabled: false, expected_revision: 4 });
  });

  test('setEnabled reports a revision conflict', async () => {
    fake.replies[`${MOD}.set_enabled`] = { conflict: true, code: 'conflict' };
    expect(await post('setEnabled', { id: 'a1', enabled: 'on', revision: '3' })).toMatchObject({
      status: 409,
      data: { ok: false, code: 'conflict', conflict: true }
    });
  });

  test('setEnabled on a vanished automation is not_found', async () => {
    fake.replies[`${MOD}.set_enabled`] = { code: 'not_found' };
    expect(await post('setEnabled', { id: 'gone', enabled: 'on', revision: '1' })).toMatchObject({
      status: 400,
      data: { code: 'not_found' }
    });
  });

  test('remove deletes by id and refuses an unknown one', async () => {
    fake.replies[`${MOD}.delete`] = { deleted: true };
    expect(await post('remove', { id: 'a1' })).toEqual({ ok: true });
    fake.replies[`${MOD}.delete`] = { deleted: false };
    expect(await post('remove', { id: 'a1' })).toMatchObject({ status: 400, data: { code: 'not_found' } });
    expect(await post('remove', {})).toMatchObject({ status: 400 });
  });

  test('run asks Sesame to run the named automation as the dashboard', async () => {
    fake.replies['bagel.rpc.automations.run'] = { accepted: true, run_id: 'r1' };
    expect(await post('run', { name: 'Hello' })).toEqual({ ok: true });
    expect(fake.calls[0].body).toMatchObject({ broadcaster_id: '7', name: 'Hello', source: 'dashboard' });
  });

  test('run surfaces the refusal code', async () => {
    fake.replies['bagel.rpc.automations.run'] = { accepted: false, code: 'forbidden' };
    expect(await post('run', { name: 'Hello' })).toMatchObject({ data: { ok: false, code: 'forbidden' } });
  });

  test.each(['toggle', 'setEnabled', 'remove', 'run'] as const)('%s refuses a free account while in beta', async (name) => {
    const res = await post(name, { id: 'a1', name: 'Hello', is_enabled: 'on', enabled: 'on' }, session, 'free');
    expect(res).toMatchObject({ status: 403, data: { error: expect.stringContaining('Premium') } });
    expect(fake.calls).toEqual([]);
  });

  test('anonymous callers are refused', async () => {
    expect(await post('remove', { id: 'a1' }, null)).toMatchObject({ status: 401 });
    expect(fake.calls).toEqual([]);
  });
});
