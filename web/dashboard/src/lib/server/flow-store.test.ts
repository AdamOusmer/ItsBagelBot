// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { alertDefinition, commandDefinition, SETUP_REWARD_ID, type Definition } from '@bagel/kit/automations';
import type { CommandView } from '@bagel/kit';

import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

const calls: Array<{ subject: string; body: any }> = [];
let replies: Record<string, unknown | ((body: any) => unknown)> = {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: any) => {
    calls.push({ subject, body });
    if (!(subject in replies)) throw new Error(`Unexpected RPC: ${subject}`);
    const reply = replies[subject];
    if (reply instanceof Error) throw reply;
    return typeof reply === 'function' ? (reply as (b: any) => unknown)(body) : reply;
  }
}));
mock.module('./edge-purge', () => ({ schedulePurgeChannel: () => {}, scheduleLeaderboardPurge: () => {} }));

const { fabric } = await import('./services');
const store = await import('./flow-store');

const account = (status: string) => ({ value: { status, active: true, onboarded: true } });
const localsFor = (status: string, session: Record<string, unknown> = { user_id: '7' }) =>
  ({ session, locale: 'en', accountState: account(status) }) as unknown as App.Locals;
const premium = localsFor('paid');
const free = localsFor('free');

const AUTO = 'bagel.rpc.modules.automation';
const hugRow: CommandView = {
  name: 'hug',
  aliases: ['cuddle'],
  response: 'hi {user}',
  is_active: true,
  stream_online_only: true,
  perm: 'mod',
  cooldown: 30,
  user_cooldown: 5,
  allowed_user_id: '99',
  bump_counter: 'hugs',
  uses: '41'
};
const alerts = (configs: Record<string, unknown>, is_enabled = true, revision = 4) => ({ name: 'alerts', is_enabled, configs, revision });
const automationsOn = { name: 'automations', is_enabled: true, configs: {}, revision: 1 };
const followFlow: Definition = {
  version: 1,
  trigger: { type: 'event', params: { kind: 'channel.follow' } },
  actions: [{ type: 'chat', params: { text: 'hi' } }, { type: 'points_add', params: { amount: 5 } }]
};
const savedAutomation = (id: string, definition: Definition, revision = 1) => ({
  id,
  user_id: 7,
  name: 'Flow',
  enabled: true,
  revision,
  definition,
  created_at: '',
  updated_at: ''
});
const hugSimple = { name: 'hug', aliases: ['cuddle'], response: 'hi {user}', perm: 'mod' as const, cooldown: 30, userCooldown: 5 };

const callsTo = (subject: string) => calls.filter((c) => c.subject === subject);
const moduleWrites = () => callsTo('bagel.rpc.modules.upsert').map((c) => ({ name: c.body.name, is_enabled: c.body.is_enabled }));

function stubStorage(commands: CommandView[], modules: unknown[]) {
  replies['bagel.rpc.commands.list'] = { commands };
  replies['bagel.rpc.modules.list'] = { modules };
  replies['bagel.rpc.modules.upsert'] = { modules };
  replies['bagel.rpc.projector.dashboard.modules.get'] = { modules };
  replies['bagel.rpc.projector.dashboard.commands.get'] = { commands };
  replies['bagel.rpc.projector.dashboard.commands.replace'] = { commands };
  replies['bagel.rpc.projector.dashboard.modules.replace'] = { modules };
  replies[`${AUTO}.list`] = { automations: [] };
}

beforeEach(() => {
  calls.length = 0;
  replies = {};
  fabric.cache.invalidate('automations:7', 'commands:7', 'modules:7');
});

describe('read-through', () => {
  test('commands and alerts load as generated flows alongside automations', async () => {
    stubStorage([hugRow, { ...hugRow, name: 'empty', response: '' }], [alerts({ followMessage: 'hey {user}', subEnabled: 'off' })]);
    replies[`${AUTO}.list`] = { automations: [savedAutomation('a', followFlow, 3)] };

    const flows = await store.listFlows('7');

    expect(flows.find((f) => f.ref.layer === 'automation')).toMatchObject({ revision: '3', definition: followFlow });
    expect(flows.find((f) => f.ref.layer === 'command')).toMatchObject({
      ref: { layer: 'command', name: 'hug' },
      enabled: true,
      definition: commandDefinition(hugSimple)
    });
    expect(flows.filter((f) => f.ref.layer === 'command')).toHaveLength(1);
    const alert = (key: string) => flows.find((f) => f.ref.layer === 'alert' && f.ref.alert === key)!;
    expect(alert('follow')).toMatchObject({ enabled: true, definition: alertDefinition('follow', 'hey {user}') });
    expect(alert('sub').enabled).toBe(false);
    expect(flows.filter((f) => f.ref.layer === 'alert')).toHaveLength(5);
  });

  test('an alert with no stored line shows the module default', async () => {
    stubStorage([], []);
    const flow = await store.loadFlow('7', { layer: 'alert', alert: 'follow' });
    expect(flow?.enabled).toBe(false);
    expect(flow?.definition.actions?.[0]?.params?.text).toContain('Thank you for following');
  });

  test('an alert line loads in the engine form and saves back in the module form', async () => {
    stubStorage([], [alerts({ cheerMessage: 'thanks for {bits}' })]);
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [alerts({ cheerMessage: 'thanks for {bits}' }, true, 5)] };
    const loaded = (await store.loadFlow('7', { layer: 'alert', alert: 'cheer' }))!;
    expect(loaded.definition.actions?.[0]?.params?.text).toBe('thanks for {event.amount}');

    const res = await store.saveFlow(premium,{ ref: loaded.ref, name: loaded.name, enabled: true, definition: loaded.definition, expectedRevision: loaded.revision });

    expect(res.ok).toBe(true);
    expect(callsTo('bagel.rpc.modules.patch')[0]!.body.configs.cheerMessage).toBe('thanks for {bits}');
  });
});

describe('saving to the row a flow came from', () => {
  test('a simple command saves back to its row and keeps fields the board cannot show', async () => {
    stubStorage([hugRow], []);
    replies['bagel.rpc.commands.upsert'] = {};
    const loaded = (await store.loadFlow('7', { layer: 'command', name: 'hug' }))!;

    const res = await store.saveFlow(premium,{
      ref: loaded.ref,
      name: loaded.name,
      enabled: true,
      definition: commandDefinition({ ...hugSimple, response: 'changed' }),
      expectedRevision: loaded.revision
    });

    expect(res.ok).toBe(true);
    expect(callsTo(`${AUTO}.upsert`)).toEqual([]);
    expect(callsTo('bagel.rpc.commands.upsert')[0]!.body).toMatchObject({
      name: 'hug',
      response: 'changed',
      stream_online_only: true,
      allowed_user_id: '99',
      bump_counter: 'hugs',
      original_name: 'hug'
    });
  });

  test('a stale command revision conflicts without writing', async () => {
    stubStorage([hugRow], []);
    const res = await store.saveFlow(premium,{
      ref: { layer: 'command', name: 'hug' },
      name: '!hug',
      enabled: true,
      definition: commandDefinition(hugSimple),
      expectedRevision: 'stale'
    });
    expect(res).toMatchObject({ ok: false, reason: 'conflict' });
    expect(callsTo('bagel.rpc.commands.upsert')).toEqual([]);
  });

  test('the revision a save returns still matches once the service omits empty fields', async () => {
    const bare: CommandView = { name: 'bare', response: 'hi', is_active: true };
    stubStorage([], []);
    replies['bagel.rpc.commands.upsert'] = {};
    const saved = await store.saveFlow(premium, { ref: null, name: '!bare', enabled: true, definition: commandDefinition({ name: 'bare', aliases: [], response: 'hi', perm: 'everyone', cooldown: 0, userCooldown: 0 }) });
    expect(saved.ok).toBe(true);
    stubStorage([bare], []);
    expect((await store.loadFlow('7', { layer: 'command', name: 'bare' }))!.revision).toBe((saved as { flow: { revision: string } }).flow.revision);
  });

  test('a uses bump alone is not a conflict', async () => {
    stubStorage([hugRow], []);
    const loaded = (await store.loadFlow('7', { layer: 'command', name: 'hug' }))!;
    stubStorage([{ ...hugRow, uses: '42' }], []);
    replies['bagel.rpc.commands.upsert'] = {};
    const res = await store.saveFlow(premium,{
      ref: loaded.ref,
      name: loaded.name,
      enabled: true,
      definition: loaded.definition,
      expectedRevision: loaded.revision
    });
    expect(res.ok).toBe(true);
  });

  test('a new simple command refuses to overwrite an existing one', async () => {
    stubStorage([hugRow], []);
    const res = await store.saveFlow(premium,{ ref: null, name: '!hug', enabled: true, definition: commandDefinition(hugSimple) });
    expect(res).toMatchObject({ ok: false, reason: 'conflict' });
  });

  test('an alert saves through a module patch with the current revision', async () => {
    stubStorage([], [alerts({ followMessage: 'old' })]);
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [alerts({ followEnabled: 'on', followMessage: 'new' }, true, 5)] };
    const loaded = (await store.loadFlow('7', { layer: 'alert', alert: 'follow' }))!;

    const res = await store.saveFlow(premium,{
      ref: loaded.ref,
      name: loaded.name,
      enabled: true,
      definition: alertDefinition('follow', 'new'),
      expectedRevision: loaded.revision
    });

    expect(res.ok).toBe(true);
    expect(callsTo('bagel.rpc.modules.patch')[0]!.body).toMatchObject({
      name: 'alerts',
      is_enabled: true,
      expected_rev: 4,
      configs: { followEnabled: 'on', followMessage: 'new' }
    });
  });

  test('another alert changing does not conflict with this one', async () => {
    stubStorage([], [alerts({ followMessage: 'old' })]);
    const loaded = (await store.loadFlow('7', { layer: 'alert', alert: 'follow' }))!;
    stubStorage([], [alerts({ followMessage: 'old', subMessage: 'changed' }, true, 9)]);
    replies['bagel.rpc.modules.patch'] = { rev: 10, modules: [] };
    const res = await store.saveFlow(premium,{
      ref: loaded.ref,
      name: loaded.name,
      enabled: true,
      definition: alertDefinition('follow', 'old'),
      expectedRevision: loaded.revision
    });
    expect(res.ok).toBe(true);
    expect(callsTo('bagel.rpc.modules.patch')[0]!.body.expected_rev).toBe(9);
  });

  test('a module patch conflict is a conflict', async () => {
    stubStorage([], [alerts({})]);
    replies['bagel.rpc.modules.patch'] = { conflict: true, rev: 6, modules: [] };
    const res = await store.saveFlow(premium,{ ref: { layer: 'alert', alert: 'raid' }, name: 'Raid', enabled: true, definition: alertDefinition('raid', 'x') });
    expect(res).toMatchObject({ ok: false, reason: 'conflict' });
  });

  test('a new follow flow refuses to replace a live alert line', async () => {
    stubStorage([], [alerts({ followMessage: 'old' })]);
    const res = await store.saveFlow(premium,{ ref: null, name: 'Follow', enabled: true, definition: alertDefinition('follow', 'new') });
    expect(res).toMatchObject({ ok: false, reason: 'conflict' });
  });
});

describe('automation rows', () => {
  test('saving an enabled recipe turns on the module the engine requires', async () => {
    stubStorage([], []);
    replies[`${AUTO}.upsert`] = { automation: savedAutomation('new', followFlow) };
    const res = await store.saveFlow(premium,{ ref: null, name: 'Flow', enabled: true, definition: followFlow });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'automation', id: 'new' }, revision: '1' }, warnings: [] });
    expect(moduleWrites()).toEqual([{ name: 'automations', is_enabled: true }]);
  });

  test('saving a disabled recipe leaves the module alone', async () => {
    stubStorage([], []);
    replies[`${AUTO}.upsert`] = { automation: savedAutomation('new', followFlow) };
    await store.saveFlow(premium,{ ref: null, name: 'Flow', enabled: false, definition: followFlow });
    expect(moduleWrites()).toEqual([]);
  });

  test('a recipe converted to a command opens right away, before the commands service flushes', async () => {
    stubStorage([], [automationsOn]);
    replies['bagel.rpc.commands.upsert'] = {};
    const res = await store.saveFlow(premium,{ ref: null, name: 'Flow', enabled: true, definition: commandDefinition(hugSimple) });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'command', name: 'hug' } } });
    expect(await store.loadFlow('7', { layer: 'command', name: 'hug' })).not.toBeNull();
  });

  test('expected revision reaches the owner and a stale one is a conflict', async () => {
    stubStorage([], [automationsOn]);
    replies[`${AUTO}.upsert`] = { conflict: true, code: 'conflict' };
    const res = await store.saveFlow(premium,{ ref: { layer: 'automation', id: 'a' }, name: 'Flow', enabled: true, definition: followFlow, expectedRevision: '3' });
    expect(res).toMatchObject({ ok: false, reason: 'conflict' });
    expect(callsTo(`${AUTO}.upsert`)[0]!.body).toMatchObject({ id: 'a', expected_revision: 3 });
  });

  test('an invalid definition never reaches a service', async () => {
    const res = await store.saveFlow(premium,{ ref: null, name: 'Flow', enabled: true, definition: { version: 1, trigger: { type: 'nope' } } });
    expect(res).toMatchObject({ ok: false, reason: 'invalid' });
    expect(calls).toEqual([]);
  });

  test('a command an automation cannot beat is flagged', async () => {
    stubStorage([hugRow], [automationsOn]);
    const definition: Definition = {
      version: 1,
      trigger: { type: 'command', params: { name: '!HUG' } },
      conditions: [{ type: 'chance', params: { percent: 50 } }],
      actions: [{ type: 'chat', params: { text: 'x' } }]
    };
    replies[`${AUTO}.upsert`] = { automation: savedAutomation('a', definition) };
    const res = await store.saveFlow(premium,{ ref: null, name: 'Flow', enabled: true, definition });
    expect(res).toMatchObject({ ok: true, warnings: ['shadowed_by_command'] });
  });
});

describe('landing list and toggles', () => {
  const summary = (id: string, preview?: string) => ({ id, name: 'Flow', enabled: true, revision: 3, trigger_type: 'event', preview, updated_at: '' });

  test('the landing list holds saved recipes only, never commands or modules', async () => {
    stubStorage([hugRow], [alerts({})]);
    replies[`${AUTO}.summaries`] = { automations: [summary('a', '{"n":[],"e":[]}'), summary('b')] };

    const flows = await store.listFlowSummaries('7');

    expect(callsTo(`${AUTO}.list`)).toEqual([]);
    expect(callsTo(`${AUTO}.get`)).toEqual([]);
    expect(flows[0]).toEqual({ ref: { layer: 'automation', id: 'a' }, name: 'Flow', enabled: true, revision: '3', preview: '{"n":[],"e":[]}' });
    expect(flows[1]!.preview).toBeNull();
    expect(flows.map((f) => f.ref.layer)).toEqual(['automation', 'automation']);
    for (const flow of flows) expect(flow).not.toHaveProperty('definition');
  });

  test('a save passes the preview along', async () => {
    stubStorage([], [automationsOn]);
    replies[`${AUTO}.upsert`] = { automation: savedAutomation('new', followFlow) };
    await store.saveFlow(premium,{ ref: null, name: 'Flow', enabled: true, definition: followFlow, preview: '{"n":[]}' });
    expect(callsTo(`${AUTO}.upsert`)[0]!.body.preview).toBe('{"n":[]}');
  });

  test('toggling an automation sends no definition and invalidates the summaries', async () => {
    stubStorage([], [automationsOn]);
    replies[`${AUTO}.summaries`] = { automations: [summary('a')] };
    await store.listFlowSummaries('7');
    replies[`${AUTO}.set_enabled`] = { automation: savedAutomation('a', followFlow, 4) };

    const res = await store.setFlowEnabled(premium,{ layer: 'automation', id: 'a' }, false, '3');

    expect(res).toEqual({ ok: true, enabled: false, revision: '4', warnings: [] });
    expect(callsTo(`${AUTO}.set_enabled`)[0]!.body).toEqual({ user_id: '7', id: 'a', enabled: false, expected_revision: 3 });
    await store.listFlowSummaries('7');
    expect(callsTo(`${AUTO}.summaries`)).toHaveLength(2);
  });

  test('toggling an automation reports conflict, missing and a switched off module', async () => {
    stubStorage([], []);
    const ref = { layer: 'automation', id: 'a' } as const;
    replies[`${AUTO}.get`] = { automation: savedAutomation('a', followFlow, 3) };
    replies[`${AUTO}.set_enabled`] = { conflict: true, code: 'conflict' };
    expect(await store.setFlowEnabled(premium,ref, true, '3')).toEqual({ ok: false, reason: 'conflict' });
    replies[`${AUTO}.set_enabled`] = { code: 'not_found', error: 'missing' };
    expect(await store.setFlowEnabled(premium,ref, true)).toEqual({ ok: false, reason: 'not_found' });
    replies[`${AUTO}.set_enabled`] = { automation: savedAutomation('a', followFlow, 4) };
    expect(await store.setFlowEnabled(premium,ref, true)).toMatchObject({ ok: true, warnings: [] });
    expect(moduleWrites()).toEqual([{ name: 'automations', is_enabled: true }]);
  });

  test('an imported recipe still holding a placeholder refuses to turn on, and may turn off', async () => {
    stubStorage([], []);
    const ref = { layer: 'automation', id: 'a' } as const;
    const awaiting = { version: 1, trigger: { type: 'reward', params: { reward_id: SETUP_REWARD_ID } }, actions: [{ type: 'chat', params: { text: 'hi' } }] };
    replies[`${AUTO}.get`] = { automation: { ...savedAutomation('a', followFlow, 3), definition: awaiting } };
    replies[`${AUTO}.set_enabled`] = { automation: savedAutomation('a', followFlow, 4) };
    expect(await store.setFlowEnabled(premium, ref, true)).toEqual({ ok: false, reason: 'needs_setup' });
    expect(callsTo(`${AUTO}.set_enabled`)).toHaveLength(0);
    expect(await store.setFlowEnabled(premium, ref, false)).toMatchObject({ ok: true });
  });

  test('saving an imported recipe switched on is refused while a placeholder remains', async () => {
    stubStorage([], []);
    const definition = { version: 1, trigger: { type: 'reward', params: { reward_id: SETUP_REWARD_ID } }, actions: [{ type: 'chat', params: { text: 'hi' } }] };
    const refused = await store.saveFlow(premium, { ref: null, name: 'x', enabled: true, definition });
    expect(refused).toEqual({ ok: false, reason: 'invalid', issues: [{ path: 'trigger', code: 'setup_required' }] });
  });

  test('a restricted command is marked so its export cannot come out open to everyone', async () => {
    stubStorage([{ name: 'hug', aliases: [], response: 'hi', is_active: true, allowed_user_id: '777000111' }], []);
    const flows = await store.listFlows('7');
    const hug = flows.find((flow) => flow.ref.layer === 'command');
    expect(hug?.restricted).toBe(true);
    expect(JSON.stringify(hug?.definition)).not.toContain('777000111');
  });

  test('toggling a command changes only is_active on its row', async () => {
    stubStorage([hugRow], []);
    replies['bagel.rpc.commands.upsert'] = {};
    const res = await store.setFlowEnabled(premium,{ layer: 'command', name: 'hug' }, false);
    expect(res).toMatchObject({ ok: true, enabled: false });
    expect(callsTo('bagel.rpc.commands.upsert')[0]!.body).toMatchObject({
      name: 'hug',
      aliases: ['cuddle'],
      response: 'hi {user}',
      is_active: false,
      perm: 'mod',
      cooldown: 30,
      user_cooldown: 5,
      stream_online_only: true,
      allowed_user_id: '99',
      bump_counter: 'hugs',
      original_name: 'hug'
    });
  });

  test('a command already in that state writes nothing', async () => {
    stubStorage([hugRow], []);
    expect(await store.setFlowEnabled(premium,{ layer: 'command', name: 'hug' }, true)).toMatchObject({ ok: true });
    expect(callsTo('bagel.rpc.commands.upsert')).toEqual([]);
  });

  test('toggling an alert patches only its switch', async () => {
    stubStorage([], [alerts({ followMessage: 'keep' })]);
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [] };
    expect(await store.setFlowEnabled(premium,{ layer: 'alert', alert: 'follow' }, false)).toMatchObject({ ok: true, warnings: [] });
    expect(callsTo('bagel.rpc.modules.patch')[0]!.body).toMatchObject({ is_enabled: true, expected_rev: 4, configs: { followEnabled: 'off' } });
    expect(callsTo('bagel.rpc.modules.patch')[0]!.body.configs).not.toHaveProperty('followMessage');
  });

  test('turning an alert on with the module off says the module came on', async () => {
    stubStorage([], [alerts({}, false)]);
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [] };
    expect(await store.setFlowEnabled(premium,{ layer: 'alert', alert: 'raid' }, true)).toMatchObject({ ok: true, warnings: ['alerts_module_turned_on'] });
  });
});

describe('a simple flow never lives in the automation table', () => {
  const simpleCommandDef = commandDefinition({ ...hugSimple, aliases: [], name: 'wave' });
  const complex: Definition = {
    version: 1,
    trigger: { type: 'command', params: { name: '!wave' } },
    conditions: [{ type: 'chance', params: { percent: 50 } }],
    actions: [{ type: 'chat', params: { text: 'x' } }]
  };
  const automation = (id: string, definition: Definition, enabled = true, revision = 3) => ({ ...savedAutomation(id, definition, revision), enabled });

  let table: Map<string, ReturnType<typeof automation>>;
  let commands: CommandView[];

  function stubTable(deleteWorks = true) {
    stubStorage(commands, [alerts({ followEnabled: 'off', cheerEnabled: 'off' }), automationsOn]);
    replies['bagel.rpc.commands.list'] = () => ({ commands });
    replies['bagel.rpc.commands.upsert'] = (body: CommandView & { original_name?: string }) => {
      commands = [...commands.filter((row) => row.name !== body.original_name && row.name !== body.name), body];
      return {};
    };
    replies[`${AUTO}.list`] = () => ({ automations: [...table.values()] });
    replies[`${AUTO}.get`] = (body: { id: string }) => (table.has(body.id) ? { automation: table.get(body.id) } : { code: 'not_found' });
    replies[`${AUTO}.upsert`] = (body: { id?: string; definition: Definition; enabled: boolean }) => {
      const id = body.id ?? 'new';
      const next = automation(id, body.definition, body.enabled, (table.get(id)?.revision ?? 0) + 1);
      table.set(id, next);
      return { automation: next };
    };
    replies[`${AUTO}.delete`] = (body: { id: string }) => {
      if (!deleteWorks) throw new Error('modules down');
      return { deleted: table.delete(body.id) };
    };
    replies[`${AUTO}.downgrade_alert`] = (body: { id: string }) => {
      table.delete(body.id);
      return {};
    };
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [] };
  }

  beforeEach(() => {
    table = new Map();
    commands = [];
    stubTable();
  });

  test('creating a simple command writes the command and no row', async () => {
    const res = await store.saveFlow(premium, { ref: null, name: 'Wave', enabled: true, definition: simpleCommandDef });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'command', name: 'wave' } }, warnings: [] });
    expect(table.size).toBe(0);
    expect(commands.map((row) => row.name)).toEqual(['wave']);
  });

  test('creating a simple alert writes the alert and no row', async () => {
    const res = await store.saveFlow(premium, { ref: null, name: 'Follow', enabled: true, definition: alertDefinition('follow', 'hi') });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'alert', alert: 'follow' } } });
    expect(table.size).toBe(0);
  });

  test('the client cannot keep a simple flow in a row by naming its ref', async () => {
    table.set('a', automation('a', complex));
    const res = await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Wave', enabled: true, definition: simpleCommandDef, expectedRevision: '3' });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'command', name: 'wave' } }, warnings: [] });
    expect(table.size).toBe(0);
    expect(commands.map((row) => row.name)).toEqual(['wave']);
  });

  test('edit-down writes the command before the row goes', async () => {
    table.set('a', automation('a', complex));
    await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Wave', enabled: true, definition: simpleCommandDef });
    const order = calls.map((c) => c.subject);
    expect(order.indexOf('bagel.rpc.commands.upsert')).toBeLessThan(order.indexOf(`${AUTO}.delete`));
  });

  test.each([true, false])('edit-down keeps enabled=%s', async (enabled) => {
    table.set('a', automation('a', complex, enabled));
    await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Wave', enabled, definition: simpleCommandDef });
    expect(commands[0]!.is_active).toBe(enabled);
  });

  test('edit-down to an alert is one owner call that carries the state and revisions', async () => {
    table.set('a', automation('a', followFlow, false));
    const res = await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Flow', enabled: false, definition: alertDefinition('cheer', 'thanks {bits}'), expectedRevision: '3' });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'alert', alert: 'cheer' } } });
    expect(table.size).toBe(0);
    expect(callsTo(`${AUTO}.downgrade_alert`)[0]!.body).toEqual({
      user_id: '7',
      id: 'a',
      enable_key: 'cheerEnabled',
      message: 'thanks {bits}',
      enabled: false,
      expected_revision: 3,
      expected_module_rev: 4
    });
    expect(callsTo('bagel.rpc.modules.patch')).toEqual([]);
  });

  test('a stale revision on edit-down changes nothing', async () => {
    table.set('a', automation('a', complex));
    const res = await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Wave', enabled: true, definition: simpleCommandDef, expectedRevision: '1' });
    expect(res).toMatchObject({ ok: false, reason: 'conflict' });
    expect(table.get('a')!.definition).toEqual(complex);
    expect(commands).toEqual([]);
  });

  test('a name collision refuses clearly and leaves the row untouched', async () => {
    commands = [{ ...hugRow, name: 'other', aliases: ['WAVE'] }];
    stubTable();
    table.set('a', automation('a', complex));
    const issues = [{ path: 'trigger.params.name', code: 'command_exists' }];
    expect(await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Wave', enabled: true, definition: simpleCommandDef })).toEqual({ ok: false, reason: 'conflict', issues });
    expect(await store.saveFlow(premium, { ref: null, name: 'Wave', enabled: true, definition: simpleCommandDef })).toEqual({ ok: false, reason: 'conflict', issues });
    expect(table.get('a')!.definition).toEqual(complex);
    expect(callsTo(`${AUTO}.upsert`)).toEqual([]);
  });

  const taken = [{ path: 'trigger', code: 'alert_exists' }];
  const defaultFollow = 'Thank you for following the channel, {user}!';

  test.each([
    ['off with a custom line', { followEnabled: 'off', followMessage: 'mine' }, 'hi', true],
    ['on with a custom line', { followMessage: 'mine' }, 'hi', true],
    ['on with the default line', {}, 'hi', true],
    ['on, and the flow would switch it off', {}, defaultFollow, false]
  ] as const)('an alert %s is never changed by another flow', async (_label, configs, line, enabled) => {
    stubStorage(commands, [alerts(configs)]);
    table.set('a', automation('a', followFlow));
    replies[`${AUTO}.get`] = () => ({ automation: table.get('a') });
    const definition = alertDefinition('follow', line);
    const refusal = { ok: false, reason: 'conflict', issues: taken };

    expect(await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Flow', enabled, definition })).toEqual(refusal as never);
    table.clear();
    replies[`${AUTO}.list`] = { automations: [] };
    expect(await store.saveFlow(premium, { ref: null, name: 'Flow', enabled, definition })).toEqual(refusal as never);
    expect(callsTo('bagel.rpc.modules.patch')).toEqual([]);
    expect(callsTo(`${AUTO}.downgrade_alert`)).toEqual([]);
  });

  test('a move from a command also respects it, and leaves the command', async () => {
    commands = [hugRow];
    stubStorage(commands, [alerts({ followEnabled: 'off', followMessage: 'mine' })]);
    const res = await store.saveFlow(premium, { ref: { layer: 'command', name: 'hug' }, name: 'Follow', enabled: true, definition: alertDefinition('follow', 'hi'), confirmUpgrade: true });
    expect(res).toEqual({ ok: false, reason: 'conflict', issues: taken });
    expect(callsTo('bagel.rpc.commands.delete')).toEqual([]);
  });

  test('saving the same line an alert already shows changes nothing, so it passes', async () => {
    stubStorage(commands, [alerts({})]);
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [] };
    const res = await store.saveFlow(premium, { ref: null, name: 'Follow', enabled: true, definition: alertDefinition('follow', defaultFollow) });
    expect(res).toMatchObject({ ok: true });
  });

  test('editing the alert itself stays allowed, custom line included', async () => {
    stubStorage(commands, [alerts({ followMessage: 'mine' })]);
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [] };
    const res = await store.saveFlow(premium, { ref: { layer: 'alert', alert: 'follow' }, name: 'Follow', enabled: false, definition: alertDefinition('follow', 'new') });
    expect(res.ok).toBe(true);
  });

  test('a row already holding that alert blocks a new flow', async () => {
    table.set('old', automation('old', alertDefinition('follow', 'old line')));
    const res = await store.saveFlow(premium, { ref: null, name: 'Follow', enabled: true, definition: alertDefinition('follow', 'hi') });
    expect(res).toEqual({ ok: false, reason: 'conflict', issues: taken });
    expect(table.has('old')).toBe(true);
  });

  test('takenAlerts names every alert another flow may not write', async () => {
    stubStorage(commands, [alerts({ followEnabled: 'off', followMessage: 'mine', subEnabled: 'off', giftEnabled: 'off', cheerEnabled: 'off' })]);
    replies[`${AUTO}.list`] = { automations: [automation('old', alertDefinition('cheer', 'old line'))] };
    expect(await store.takenAlerts('7')).toEqual(['follow', 'cheer', 'raid']);
  });

  test('a customised alert refuses edit-down, an unused one on its default line is taken over', async () => {
    stubStorage(commands, [alerts({ followMessage: 'mine' })]);
    table.set('a', automation('a', followFlow));
    replies[`${AUTO}.get`] = { automation: table.get('a') };
    const refused = await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Flow', enabled: true, definition: alertDefinition('follow', 'hi') });
    expect(refused).toEqual({ ok: false, reason: 'conflict', issues: [{ path: 'trigger', code: 'alert_exists' }] });

    stubStorage(commands, [alerts({ followEnabled: 'off' })]);
    replies[`${AUTO}.get`] = { automation: table.get('a') };
    replies[`${AUTO}.list`] = { automations: [table.get('a')] };
    replies[`${AUTO}.downgrade_alert`] = {};
    const taken = await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Flow', enabled: true, definition: alertDefinition('follow', 'hi') });
    expect(taken.ok).toBe(true);
  });

  test('a failed delete is flagged, shadowed by the command, and retried on the next save', async () => {
    stubTable(false);
    table.set('a', automation('a', complex));
    const first = await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Wave', enabled: true, definition: simpleCommandDef });
    expect(first).toMatchObject({ ok: true, flow: { ref: { layer: 'command', name: 'wave' } }, warnings: ['automation_delete_failed'] });
    expect(table.get('a')!.definition).toEqual(simpleCommandDef);

    stubTable();
    if (first.ok) {
      const second = await store.saveFlow(premium, { ref: first.flow.ref, name: 'Wave', enabled: true, definition: simpleCommandDef, expectedRevision: first.flow.revision });
      expect(second).toMatchObject({ ok: true, warnings: [] });
    }
    expect(table.size).toBe(0);
  });

  test('a failed command write restores the row', async () => {
    table.set('a', automation('a', complex));
    replies['bagel.rpc.commands.upsert'] = new Error('commands down');
    const res = await store.saveFlow(premium, { ref: { layer: 'automation', id: 'a' }, name: 'Wave', enabled: true, definition: simpleCommandDef });
    expect(res).toMatchObject({ ok: false, reason: 'failed' });
    expect(table.get('a')!.definition).toEqual(complex);
  });

  test('a command edited into an alert skips the table', async () => {
    commands = [hugRow];
    stubTable();
    replies['bagel.rpc.commands.delete'] = {};
    const res = await store.saveFlow(premium, { ref: { layer: 'command', name: 'hug' }, name: 'Follow', enabled: true, definition: alertDefinition('follow', 'hi'), confirmUpgrade: true });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'alert', alert: 'follow' } }, upgrade: { from: 'command', uses: 41 } });
    expect(table.size).toBe(0);
    expect(callsTo('bagel.rpc.commands.delete')).toHaveLength(1);
  });

  test('an alert edited into a command skips the table and turns the alert off', async () => {
    stubStorage(commands, [alerts({})]);
    replies['bagel.rpc.commands.upsert'] = {};
    replies['bagel.rpc.modules.patch'] = { rev: 5, modules: [] };
    const res = await store.saveFlow(premium, { ref: { layer: 'alert', alert: 'raid' }, name: 'Wave', enabled: true, definition: simpleCommandDef, confirmUpgrade: true });
    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'command', name: 'wave' } } });
    expect(callsTo(`${AUTO}.upsert`)).toEqual([]);
    expect(callsTo('bagel.rpc.modules.patch')[0]!.body.configs).toEqual({ raidEnabled: 'off' });
  });

  test('a misplaced row with the same command is cleaned up by saving that command', async () => {
    commands = [{ ...hugRow, name: 'wave', aliases: [] }];
    stubTable();
    table.set('old', automation('old', simpleCommandDef));
    table.set('real', automation('real', complex));
    await store.saveFlow(premium, { ref: { layer: 'command', name: 'wave' }, name: '!wave', enabled: true, definition: simpleCommandDef });
    expect([...table.keys()]).toEqual(['real']);
  });

  test('only the flow store and the form editor can write automation rows, and the form editor refuses simple flows', () => {
    const root = join(import.meta.dir, '../..');
    const sources = (dir: string): string[] =>
      readdirSync(dir).flatMap((name) => {
        const path = join(dir, name);
        if (statSync(path).isDirectory()) return sources(path);
        return /\.(ts|svelte)$/.test(name) && !/\.test\.ts$/.test(name) ? [path] : [];
      });
    const writers = sources(root)
      .filter((path) => /\bupsertAutomation\s*\(/.test(readFileSync(path, 'utf8')) || /\.automation\.upsert\b/.test(readFileSync(path, 'utf8')))
      .map((path) => path.slice(root.length + 1))
      .sort();
    expect(writers).toEqual(['lib/server/automations-actions.ts', 'lib/server/flow-store.ts']);
    expect(readFileSync(join(root, 'lib/server/automations-actions.ts'), 'utf8')).toContain("classifyFlow(draft.definition).layer !== 'automation'");
  });
});

describe('premium beta', () => {
  const denied = { ok: false as const, reason: 'denied' as const, error: expect.stringContaining('Premium') as unknown as string };

  test('every write refuses a free account before reaching a service', async () => {
    const saves = [
      { ref: null, name: 'Flow', enabled: true, definition: followFlow },
      { ref: { layer: 'automation', id: 'a' } as const, name: 'Flow', enabled: true, definition: followFlow },
      { ref: { layer: 'command', name: 'hug' } as const, name: '!hug', enabled: true, definition: commandDefinition(hugSimple) },
      { ref: { layer: 'command', name: 'hug' } as const, name: 'Hug', enabled: true, definition: followFlow, confirmUpgrade: true },
      { ref: { layer: 'alert', alert: 'follow' } as const, name: 'Follow', enabled: true, definition: alertDefinition('follow', 'x') },
      { ref: { layer: 'alert', alert: 'follow' } as const, name: 'Follow', enabled: true, definition: followFlow, confirmUpgrade: true }
    ];
    for (const input of saves) expect(await store.saveFlow(free, input)).toEqual(denied);
    expect(await store.setFlowEnabled(free, { layer: 'automation', id: 'a' }, false)).toEqual(denied);
    expect(await store.setFlowEnabled(free, { layer: 'command', name: 'hug' }, false)).toEqual(denied);
    expect(await store.setFlowEnabled(free, { layer: 'alert', alert: 'raid' }, false)).toEqual(denied);
    expect(await store.deleteFlow(free, { layer: 'automation', id: 'a' })).toEqual(denied);
    expect(calls).toEqual([]);
  });

  test('reads stay open and writes work again with Premium', async () => {
    stubStorage([hugRow], []);
    replies[`${AUTO}.summaries`] = { automations: [{ id: 'a', name: 'Flow', enabled: true, revision: 3, trigger_type: 'event', updated_at: '' }] };
    expect((await store.listFlowSummaries('7')).length).toBeGreaterThan(0);
    expect(await store.loadFlow('7', { layer: 'command', name: 'hug' })).not.toBeNull();
    expect(await store.setFlowEnabled(localsFor('vip'), { layer: 'command', name: 'hug' }, true)).toMatchObject({ ok: true });
  });

  test('a delegate acts on the channel they manage, gated by that channel', async () => {
    stubStorage([hugRow], []);
    replies['bagel.rpc.commands.upsert'] = {};
    replies['bagel.rpc.dashboard.state_get'] = { active: true, status: 'paid' };
    const delegate = { session: { user_id: '9', delegate_of: '7', sections: ['modules'] }, locale: 'en' } as unknown as App.Locals;
    expect(await store.setFlowEnabled(delegate, { layer: 'command', name: 'hug' }, false)).toMatchObject({ ok: true });
    expect(callsTo('bagel.rpc.commands.upsert')[0]!.body.user_id).toBe('7');
  });
});

describe('deleting', () => {
  test('an automation row goes through the owner', async () => {
    replies[`${AUTO}.delete`] = { deleted: true };
    expect(await store.deleteFlow(premium, { layer: 'automation', id: 'a' })).toEqual({ ok: true });
    replies[`${AUTO}.delete`] = { deleted: false };
    expect(await store.deleteFlow(premium, { layer: 'automation', id: 'a' })).toEqual({ ok: false, reason: 'not_found' });
  });

  test('a command row is deleted if it exists', async () => {
    stubStorage([hugRow], []);
    replies['bagel.rpc.commands.delete'] = {};
    expect(await store.deleteFlow(premium, { layer: 'command', name: 'hug' })).toEqual({ ok: true });
    expect(await store.deleteFlow(premium, { layer: 'command', name: 'ghost' })).toEqual({ ok: false, reason: 'not_found' });
  });

  test('an alert has no row of its own to delete', async () => {
    expect(await store.deleteFlow(premium, { layer: 'alert', alert: 'follow' })).toEqual({ ok: false, reason: 'not_deletable' });
  });
});

describe('upgrades', () => {
  const complexHug: Definition = {
    version: 1,
    trigger: { type: 'command', params: { name: '!hug' } },
    conditions: [{ type: 'chance', params: { percent: 85 } }],
    actions: [{ type: 'chat', params: { text: 'x' } }]
  };
  const commandInput = { ref: { layer: 'command', name: 'hug' } as const, name: 'Hug', enabled: true, definition: complexHug };

  test('a command never upgrades without confirmation and reports what would go', async () => {
    stubStorage([hugRow], [automationsOn]);
    const res = await store.saveFlow(premium,commandInput);
    expect(res).toEqual({
      ok: false,
      reason: 'needs_upgrade',
      upgrade: { from: 'command', uses: 41, usesTokenInReply: false, streamOnlineOnly: true, allowedUserId: '99', bumpCounter: 'hugs' }
    });
    expect(calls.filter((c) => c.subject.endsWith('.upsert') || c.subject.endsWith('.delete'))).toEqual([]);
  });

  test('a confirmed command upgrade writes the automation before deleting the command', async () => {
    stubStorage([hugRow], [automationsOn]);
    replies[`${AUTO}.upsert`] = { automation: savedAutomation('a', complexHug) };
    replies['bagel.rpc.commands.delete'] = {};

    const res = await store.saveFlow(premium,{ ...commandInput, confirmUpgrade: true });

    expect(res).toMatchObject({ ok: true, flow: { ref: { layer: 'automation', id: 'a' } }, upgrade: { from: 'command', uses: 41 }, warnings: [] });
    const order = calls.map((c) => c.subject);
    expect(order.indexOf(`${AUTO}.upsert`)).toBeLessThan(order.indexOf('bagel.rpc.commands.delete'));
  });

  test('a refused automation leaves the command in place', async () => {
    stubStorage([hugRow], [automationsOn]);
    replies[`${AUTO}.upsert`] = { issues: [{ path: 'name', code: 'duplicate' }], code: 'conflict' };
    const res = await store.saveFlow(premium,{ ...commandInput, confirmUpgrade: true });
    expect(res).toMatchObject({ ok: false, reason: 'invalid' });
    expect(callsTo('bagel.rpc.commands.delete')).toEqual([]);
  });

  test('a reply using the uses token is flagged and cannot move to an automation', async () => {
    stubStorage([{ ...hugRow, response: 'used {count} times' }], [automationsOn]);
    const definition: Definition = { ...complexHug, actions: [{ type: 'chat', params: { text: 'used {count} times' } }] };
    const input = { ...commandInput, definition };
    expect(await store.saveFlow(premium,input)).toMatchObject({ reason: 'needs_upgrade', upgrade: { usesTokenInReply: true } });
    expect(await store.saveFlow(premium,{ ...input, confirmUpgrade: true })).toEqual({
      ok: false,
      reason: 'invalid',
      issues: [{ path: 'actions', code: 'uses_token' }]
    });
    expect(callsTo(`${AUTO}.upsert`)).toEqual([]);
    expect(callsTo('bagel.rpc.commands.delete')).toEqual([]);
  });

  test('a failed delete after the write is reported, not hidden', async () => {
    stubStorage([hugRow], [automationsOn]);
    replies[`${AUTO}.upsert`] = { automation: savedAutomation('a', complexHug) };
    replies['bagel.rpc.commands.delete'] = new Error('down');
    const res = await store.saveFlow(premium,{ ...commandInput, confirmUpgrade: true });
    expect(res).toMatchObject({ ok: true, warnings: ['command_delete_failed'] });
  });

  const alertInput = { ref: { layer: 'alert', alert: 'follow' } as const, name: 'Follow', enabled: true, definition: followFlow };

  test('an alert never upgrades without confirmation and names the lost repeat guard', async () => {
    stubStorage([], [alerts({}), automationsOn]);
    const res = await store.saveFlow(premium,alertInput);
    expect(res).toEqual({
      ok: false,
      reason: 'needs_upgrade',
      upgrade: { from: 'alert', alert: 'follow', repeatGuardWindowHours: 72, resubsNoLongerCovered: false }
    });
  });

  test('a confirmed alert upgrade is one owner call with the module revision', async () => {
    stubStorage([], [alerts({}), automationsOn]);
    replies[`${AUTO}.upgrade_alert`] = { automation: savedAutomation('a', followFlow) };

    const res = await store.saveFlow(premium,{ ...alertInput, confirmUpgrade: true });

    expect(res).toMatchObject({ ok: true, upgrade: { from: 'alert', alert: 'follow' }, warnings: [] });
    expect(callsTo(`${AUTO}.upgrade_alert`)[0]!.body).toMatchObject({ enable_key: 'followEnabled', expected_module_rev: 4 });
    expect(callsTo('bagel.rpc.modules.patch')).toEqual([]);
    expect(callsTo(`${AUTO}.upsert`)).toEqual([]);
  });

  test('a stale module revision on upgrade is a conflict', async () => {
    stubStorage([], [alerts({}), automationsOn]);
    replies[`${AUTO}.upgrade_alert`] = { conflict: true, code: 'conflict' };
    const res = await store.saveFlow(premium,{ ...alertInput, confirmUpgrade: true });
    expect(res).toMatchObject({ ok: false, reason: 'conflict' });
  });

  test('a sub alert upgrade warns that resubs drop out', async () => {
    stubStorage([], [alerts({})]);
    const definition: Definition = {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.subscribe' } },
      actions: [{ type: 'chat', params: { text: 'a' } }, { type: 'chat', params: { text: 'b' } }]
    };
    const res = await store.saveFlow(premium,{ ref: { layer: 'alert', alert: 'sub' }, name: 'Sub', enabled: true, definition });
    expect(res).toMatchObject({ reason: 'needs_upgrade', upgrade: { resubsNoLongerCovered: true, repeatGuardWindowHours: 0.25 } });
  });
});
