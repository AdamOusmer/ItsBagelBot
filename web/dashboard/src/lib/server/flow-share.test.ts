// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { FLOW_FILE_FORMAT, MAX_FLOW_FILE_BYTES, type Flow } from '@bagel/kit/automations';
import { installAutomationsRpcFake } from '../../../test/automations-rpc';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let allowed = true;
const limiterKeys: string[] = [];
mock.module('@bagel/kit/server/rate-limit', () => ({
  ValkeyRateLimiter: class {
    async check(key: string) {
      limiterKeys.push(key);
      return { allowed, retryAfterSec: 1 };
    }
  }
}));

const saves: { locals: unknown; input: any }[] = [];
const created = (input: any) => ({ ok: true, flow: { ref: input.ref ?? { layer: 'automation', id: 'new' }, name: input.name, enabled: false, revision: '1', definition: input.definition }, warnings: [] });
let saveReply: (input: any) => unknown = created;
const listFlowsCalls: string[] = [];
const chat = (text: string) => ({ type: 'chat', params: { text } });
const manual = { version: 1, trigger: { type: 'manual', params: {} }, actions: [chat('hi')] };
const drawable = { version: 1, trigger: { type: 'command', params: { name: '!fresh' } }, actions: [chat('hi'), chat('there')] };
mock.module('./flow-store', () => ({
  listFlows: async (uid: string) => {
    listFlowsCalls.push(uid);
    return [
      { ref: { layer: 'automation', id: 'a1' }, name: 'First', enabled: true, revision: '1', definition: { ...manual, actions: [chat('one'), { type: 'run', params: { automation_id: 'a2' } }] } },
      { ref: { layer: 'automation', id: 'a2' }, name: 'Second', enabled: true, revision: '1', definition: manual },
      { ref: { layer: 'command', name: 'hug' }, name: '!hug', enabled: true, revision: 'x', definition: manual },
      { ref: { layer: 'automation', id: 'a3' }, name: 'Taken', enabled: true, revision: '1', definition: manual }
    ];
  },
  takenAlerts: async () => ['follow'],
  saveFlow: async (locals: unknown, input: any) => {
    saves.push({ locals, input });
    return saveReply(input);
  }
}));
mock.module('./commands-store', () => ({ listCommands: async () => [{ name: 'hug', aliases: ['cuddle'], response: 'x', is_active: true }] }));

const fake = await installAutomationsRpcFake();
const { flowShareActions, regeneratedPreview } = await import('./flow-share');
const { SUB } = await import('./services');

const flowPreview = (flow: Flow) => ({ steps: flow.nodes.length });
const actions = flowShareActions({ flowPreview });
const owner = { user_id: '7' };

function post(name: keyof typeof actions, form: FormData, session: unknown = owner, status = 'paid') {
  const request = new Request('https://dashboard.itsbagelbot.com/flows', { method: 'POST', body: form });
  const locals = { session, locale: 'en', accountState: { value: { status } } };
  return actions[name]({ request, locals } as never) as Promise<any>;
}

const fileForm = (text: string, indexes: number[] = []) => {
  const form = new FormData();
  form.set('file', new File([text], 'recipes.json', { type: 'application/json' }));
  for (const index of indexes) form.append('index', String(index));
  return form;
};

const exportForm = (...keys: string[]) => {
  const form = new FormData();
  for (const key of keys) form.append('flow', key);
  return form;
};

const file = (flows: unknown[]) => JSON.stringify({ format: FLOW_FILE_FORMAT, version: 1, flows });
const saved = () => saves.map((save) => save.input);

beforeEach(() => {
  fake.reset();
  allowed = true;
  saveReply = created;
  saves.length = 0;
  limiterKeys.length = 0;
  listFlowsCalls.length = 0;
});

describe('gates', () => {
  const delegate = (sections: string[]) => ({ user_id: '9', delegate_of: '7', sections });

  test('a free account is refused on every seam', async () => {
    for (const name of ['flowExport', 'flowImportPreview', 'flowImportCommit'] as const) {
      const result = await post(name, exportForm('automation:a1'), owner, 'free');
      expect(result.status).toBe(403);
    }
    expect(listFlowsCalls).toEqual([]);
  });

  test('not signed in is refused', async () => {
    expect((await post('flowExport', exportForm('automation:a1'), null)).status).toBe(401);
  });

  test('a delegate without the modules section is refused', async () => {
    expect((await post('flowImportPreview', fileForm('{}'), delegate(['commands']))).status).toBe(403);
  });

  test('a delegate with the section works on the broadcaster, and is limited under the broadcaster', async () => {
    fake.replies[`${SUB.dashboard}.state_get`] = { status: 'paid' };
    const result = await post('flowExport', exportForm('automation:a1'), delegate(['modules']));
    expect(result.ok).toBe(true);
    expect(listFlowsCalls).toEqual(['7']);
    expect(limiterKeys).toEqual(['automations:7']);
  });

  test('the rate limit answers 429 with a code', async () => {
    allowed = false;
    const result = await post('flowImportCommit', fileForm('{}', [0]));
    expect(result.status).toBe(429);
    expect(result.data.code).toBe('rate_limited');
  });
});

describe('flowExport', () => {
  test('only picked recipes from this broadcaster reach the file, foreign keys count as missing', async () => {
    const result = await post('flowExport', exportForm('automation:a1', 'automation:a2', 'command:hug', 'automation:someone-elses'));
    const parsed = JSON.parse(result.file);
    expect(parsed.flows.map((f: { name: string }) => f.name)).toEqual(['First', 'Second', '!hug']);
    expect(result.file).not.toContain('Taken');
    expect(result.missing).toBe(1);
    expect(parsed.flows[0].definition.actions[1].params.automation_id).toBe('f2');
    expect(result.file).not.toContain('"a2"');
  });

  test('nothing picked is refused', async () => {
    expect((await post('flowExport', exportForm())).status).toBe(400);
  });
});

describe('flowImportPreview', () => {
  test('reports without writing and without leaking definitions', async () => {
    const result = await post('flowImportPreview', fileForm(file([{ ref: 'f1', name: 'Taken', definition: manual }, { ref: 'f2', name: 'x', definition: { version: 9 } }])));
    expect(result.entries.map((e: { status: string; name: string }) => [e.status, e.name])).toEqual([
      ['ready', 'Taken (2)'],
      ['invalid', 'x']
    ]);
    expect(result.entries[0].definition).toBeUndefined();
    expect(result.slots).toBe(97);
    expect(saved()).toEqual([]);
  });

  test('says where each lands, and treats taken alerts and aliased commands as conflicts', async () => {
    const event = (kind: string) => ({ version: 1, trigger: { type: 'event', params: { kind } }, actions: [chat('hi')] });
    const command = (name: string) => ({ version: 1, trigger: { type: 'command', params: { name } }, actions: [chat('hi')] });
    const result = await post(
      'flowImportPreview',
      fileForm(
        file([
          { ref: 'f1', name: 'taken', definition: event('channel.follow') },
          { ref: 'f2', name: 'free alert', definition: event('channel.cheer') },
          { ref: 'f3', name: 'alias', definition: command('!cuddle') },
          { ref: 'f4', name: 'free', definition: command('!free') }
        ])
      )
    );
    expect(result.entries.map((e: any) => [e.status, e.landing, e.target])).toEqual([
      ['conflict', 'alert', 'follow'],
      ['ready', 'alert', 'cheer'],
      ['conflict', 'command', 'cuddle'],
      ['ready', 'command', 'free']
    ]);
  });

  test('a bad file is one clear refusal', async () => {
    const result = await post('flowImportPreview', fileForm(JSON.stringify({ format: FLOW_FILE_FORMAT, version: 99, flows: [] })));
    expect(result.status).toBe(400);
    expect(result.data.issues).toEqual([{ path: 'version', code: 'unsupported_version' }]);
  });

  test('an oversized upload is refused before it is read', async () => {
    const result = await post('flowImportPreview', fileForm(' '.repeat(MAX_FLOW_FILE_BYTES + 1)));
    expect(result.status).toBe(413);
  });

  test('a missing file is refused', async () => {
    expect((await post('flowImportPreview', new FormData())).status).toBe(400);
  });
});

describe('flowImportCommit', () => {
  test('writes turned off, with a regenerated preview, even when the client lies', async () => {
    const form = fileForm(file([{ ref: 'f1', name: 'Fresh', definition: drawable }]), [0]);
    form.set('enabled', 'on');
    form.set('preview', '{"n":[[9,9,9,9]]}');
    const result = await post('flowImportCommit', form);
    expect(result.results[0].status).toBe('created');
    expect(saved()).toMatchObject([{ ref: null, name: 'Fresh', enabled: false, definition: drawable, preview: JSON.stringify({ steps: 3 }) }]);
  });

  test('a simple recipe goes through saveFlow and comes back as a command, off', async () => {
    const simple = { version: 1, trigger: { type: 'command', params: { name: '!simple' } }, actions: [chat('hi')] };
    saveReply = (input) => ({ ok: true, flow: { ref: { layer: 'command', name: 'simple' }, name: '!simple', enabled: false, revision: 'x', definition: input.definition }, warnings: [] });
    const result = await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'y', definition: simple }]), [0]));
    expect(result.results[0]).toMatchObject({ status: 'created', landing: 'command', target: 'simple' });
    expect(result.results[0].id).toBeUndefined();
    expect(saved()).toMatchObject([{ ref: null, enabled: false, definition: simple }]);
  });

  test('saveFlow gets the request locals, so its own gate still runs', async () => {
    await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'y', definition: manual }]), [0]));
    expect((saves[0]!.locals as { session: unknown }).session).toEqual(owner);
  });

  test('a refused save is reported per recipe, and a conflict with no detail still says so', async () => {
    saveReply = () => ({ ok: false, reason: 'conflict', issues: [] });
    const conflict = await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'y', definition: manual }]), [0]));
    expect(conflict.results[0]).toMatchObject({ status: 'failed', issues: [{ path: '', code: 'duplicate' }] });
    saveReply = () => ({ ok: false, reason: 'failed', error: 'down' });
    const down = await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'y', definition: manual }]), [0]));
    expect(down.results[0]).toMatchObject({ status: 'failed', issues: [{ path: '', code: 'write_failed' }] });
  });

  test('a file carrying a preview or enabled flag is refused per recipe', async () => {
    const result = await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'y', definition: manual, enabled: true, preview: 'x' }]), [0]));
    expect(result.results[0].status).toBe('invalid');
    expect(saved()).toEqual([]);
  });

  test('the modules service refusal is reported per recipe', async () => {
    saveReply = () => ({ ok: false, reason: 'invalid', issues: [{ path: '', code: 'too_many' }] });
    const result = await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'y', definition: manual }]), [0]));
    expect(result.results[0].status).toBe('failed');
    expect(result.results[0].issues).toEqual([{ path: '', code: 'too_many' }]);
  });

  test('no selection is refused', async () => {
    expect((await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'y', definition: manual }])))).status).toBe(400);
  });

  test('the plan is recomputed: only selected indexes are written', async () => {
    const result = await post('flowImportCommit', fileForm(file([{ ref: 'f1', name: 'a', definition: manual }, { ref: 'f2', name: 'b', definition: manual }]), [1]));
    expect(result.results.map((r: { status: string }) => r.status)).toEqual(['skipped', 'created']);
    expect(saved().map((input) => input.name)).toEqual(['b']);
  });
});

describe('regeneratedPreview', () => {
  test('a definition the board cannot draw has no preview', () => {
    const definition = { version: 1, trigger: { type: 'timer', params: { interval_s: 60 } } };
    expect(regeneratedPreview(flowPreview, definition, 'x')).toBeUndefined();
  });

  test('a preview over the stored limit is dropped', () => {
    expect(regeneratedPreview(() => 'x'.repeat(5000), drawable, 'x')).toBeUndefined();
  });
});
