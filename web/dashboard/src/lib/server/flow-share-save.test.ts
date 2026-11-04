// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { alertDefinition, FLOW_FILE_FORMAT, plainAlertLine, type Flow } from '@bagel/kit/automations';
import { moduleDef, MOD } from '@bagel/kit/catalog';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

const calls: Array<{ subject: string; body: any }> = [];
let replies: Record<string, unknown> = {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: any) => {
    calls.push({ subject, body });
    if (!(subject in replies)) throw new Error(`Unexpected RPC: ${subject}`);
    const reply = replies[subject];
    return typeof reply === 'function' ? (reply as (b: any) => unknown)(body) : reply;
  }
}));
mock.module('./edge-purge', () => ({ schedulePurgeChannel: () => {}, scheduleLeaderboardPurge: () => {} }));
mock.module('@bagel/kit/server/rate-limit', () => ({
  ValkeyRateLimiter: class {
    async check() {
      return { allowed: true, retryAfterSec: 0 };
    }
  }
}));

const { fabric } = await import('./services');
const { flowShareActions } = await import('./flow-share');

const AUTO = 'bagel.rpc.modules.automation';
const PATCH = 'bagel.rpc.modules.patch';
const COMMAND_UPSERT = 'bagel.rpc.commands.upsert';
const actions = flowShareActions({ flowPreview: (flow: Flow) => ({ steps: flow.nodes.length }) });

const chat = (text: string) => ({ type: 'chat', params: { text } });
const command = (name: string, extra: unknown[] = []) => ({ version: 1, trigger: { type: 'command', params: { name } }, actions: [chat('hi {user}'), ...extra] });
const follow = (text: string) => ({ version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, actions: [chat(text)] });

const callsTo = (subject: string) => calls.filter((call) => call.subject === subject);
const flowFile = (flows: unknown[]) => JSON.stringify({ format: FLOW_FILE_FORMAT, version: 1, flows });

function stubChannel(modules: unknown[], commands: unknown[] = []) {
  replies['bagel.rpc.commands.list'] = { commands };
  replies['bagel.rpc.modules.list'] = { modules };
  replies['bagel.rpc.projector.dashboard.modules.get'] = { modules };
  replies['bagel.rpc.projector.dashboard.commands.get'] = { commands };
  replies['bagel.rpc.projector.dashboard.commands.replace'] = { commands };
  replies['bagel.rpc.projector.dashboard.modules.replace'] = { modules };
  replies[COMMAND_UPSERT] = {};
  replies[`${AUTO}.list`] = { automations: [] };
  let next = 0;
  replies[`${AUTO}.upsert`] = (body: any) => ({
    automation: { id: body.id ?? `new-${++next}`, user_id: 7, name: body.name, enabled: body.enabled, revision: body.id ? body.expected_revision + 1 : 1, definition: body.definition }
  });
}

const alerts = (configs: Record<string, unknown>, is_enabled = true) => ({ name: 'alerts', is_enabled, configs, revision: 4 });

function commit(flows: unknown[], indexes: number[]) {
  const form = new FormData();
  form.set('file', new File([flowFile(flows)], 'recipes.json', { type: 'application/json' }));
  for (const index of indexes) form.append('index', String(index));
  const locals = { session: { user_id: '7' }, locale: 'en', accountState: { value: { status: 'paid', active: true, onboarded: true } } };
  const request = new Request('https://dashboard.itsbagelbot.com/flows', { method: 'POST', body: form });
  return actions.flowImportCommit({ request, locals } as never) as Promise<any>;
}

beforeEach(() => {
  calls.length = 0;
  replies = {};
  fabric.cache.invalidate('automations:7', 'commands:7', 'modules:7');
});

describe('imports through the real saveFlow', () => {
  test('a simple command writes a command row, off, and no automation', async () => {
    stubChannel([]);
    const { results } = await commit([{ ref: 'f1', name: 'Hug', definition: command('!hug') }], [0]);
    expect(results[0]).toMatchObject({ status: 'created', landing: 'command', target: 'hug' });
    expect(callsTo(COMMAND_UPSERT)).toHaveLength(1);
    expect(callsTo(COMMAND_UPSERT)[0]!.body).toMatchObject({ name: 'hug', response: 'hi {user}', is_active: false });
    expect(callsTo(`${AUTO}.upsert`)).toEqual([]);
  });

  test('a free alert writes module settings, off, and no automation', async () => {
    stubChannel([alerts({ followEnabled: 'off' })]);
    replies[PATCH] = { rev: 5, modules: [alerts({ followEnabled: 'off', followMessage: 'welcome' })] };
    const { results } = await commit([{ ref: 'f1', name: 'Welcome', definition: follow('welcome') }], [0]);
    expect(results[0]).toMatchObject({ status: 'created', landing: 'alert', target: 'follow' });
    expect(callsTo(PATCH)[0]!.body.configs).toMatchObject({ followEnabled: 'off', followMessage: 'welcome' });
    expect(callsTo(`${AUTO}.upsert`)).toEqual([]);
  });

  test('a taken alert never reaches the module patch', async () => {
    stubChannel([alerts({ followEnabled: 'off', followMessage: 'mine' })]);
    const custom = await commit([{ ref: 'f1', name: 'Welcome', definition: follow('welcome') }], [0]);
    expect(custom.results[0]).toMatchObject({ status: 'conflict', issues: [{ path: 'trigger', code: 'alert_exists' }] });

    fabric.cache.invalidate('automations:7', 'commands:7', 'modules:7');
    stubChannel([alerts({})]);
    const live = await commit([{ ref: 'f1', name: 'Welcome', definition: follow('welcome {user}') }], [0]);
    expect(live.results[0].status).toBe('conflict');
    expect(callsTo(PATCH)).toEqual([]);
  });

  test('an older automation row holding the alert is taken, so the import cannot sweep it', async () => {
    stubChannel([alerts({ followEnabled: 'off' })]);
    replies[`${AUTO}.list`] = { automations: [{ id: 'old', user_id: 7, name: 'Old', enabled: true, revision: 1, definition: follow('older row') }] };
    const { results } = await commit([{ ref: 'f1', name: 'Welcome', definition: follow('welcome') }], [0]);
    expect(results[0].status).toBe('conflict');
    expect(callsTo(PATCH)).toEqual([]);
    expect(callsTo(`${AUTO}.delete`)).toEqual([]);
  });

  test('a default alert exported from another channel lands as an alert in the plain line form', async () => {
    stubChannel([alerts({ followEnabled: 'off' })]);
    const line = moduleDef(MOD.alerts)!.replies.find((reply) => reply.key === 'follow')!.defaultMessage!;
    replies[PATCH] = { rev: 5, modules: [alerts({ followEnabled: 'off', followMessage: plainAlertLine(line) })] };
    const { results } = await commit([{ ref: 'f1', name: 'Follow', definition: alertDefinition('follow', line) }], [0]);
    expect(results[0]).toMatchObject({ status: 'created', landing: 'alert', target: 'follow' });
    expect(callsTo(PATCH)[0]!.body.configs.followMessage).toBe(plainAlertLine(line));
  });

  test('a flow with a run link stays an automation through both passes, off, with a preview', async () => {
    stubChannel([]);
    const definition = command('!a', [{ type: 'run', params: { automation_id: 'f2' } }]);
    const { results } = await commit(
      [
        { ref: 'f1', name: 'A', definition },
        { ref: 'f2', name: 'B', definition: command('!b', [chat('more')]) }
      ],
      [0, 1]
    );
    expect(results.map((r: any) => [r.status, r.landing, r.id])).toEqual([
      ['created', 'automation', 'new-1'],
      ['created', 'automation', 'new-2']
    ]);
    const writes = callsTo(`${AUTO}.upsert`).map((call) => call.body);
    expect(writes.every((write) => write.enabled === false)).toBe(true);
    expect(writes[0].definition.actions[1]).toEqual({ type: 'stop', enabled: false });
    expect(writes[2]).toMatchObject({ id: 'new-1', expected_revision: 1 });
    expect(writes[2].definition.actions[1]).toEqual({ type: 'run', params: { automation_id: 'new-2' } });
    expect(callsTo(COMMAND_UPSERT)).toEqual([]);
  });

  test('a free account is stopped by the gate before any write', async () => {
    stubChannel([]);
    const form = new FormData();
    form.set('file', new File([flowFile([{ ref: 'f1', name: 'Hug', definition: command('!hug') }])], 'recipes.json'));
    form.append('index', '0');
    const locals = { session: { user_id: '7' }, locale: 'en', accountState: { value: { status: 'free' } } };
    const request = new Request('https://dashboard.itsbagelbot.com/flows', { method: 'POST', body: form });
    const result = (await actions.flowImportCommit({ request, locals } as never)) as any;
    expect(result.status).toBe(403);
    expect(callsTo(COMMAND_UPSERT)).toEqual([]);
  });
});
