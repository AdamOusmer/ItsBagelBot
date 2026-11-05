// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, test } from 'bun:test';
import { actionRegistry, conditionRegistry, register, triggerRegistry } from './engine';
import { definitionToFlow, flowToDefinition } from './flow-graph';
import { exportFlows, FLOW_FILE_FORMAT, FLOW_FILE_VERSION, planFlowImport, SETUP_LOGIN, SETUP_URL, setupGaps, SHARE_REVIEWED, SHARE_REWRITTEN, type FlowImportEntry } from './flow-share';
import { ACTION_TYPES, CONDITION_TYPES, TRIGGER_TYPES } from './catalog';
import type { Definition } from './types';
import './validate';

const LEAK = 'planted-leak-marker';
const EMPTY = { count: 0, names: [], commands: [], alerts: [] };

const chat = (text: string) => ({ type: 'chat', params: { text } });
const deviceParams = {
  device: `AB:CD:EF:01:23:45:67:89-${LEAK}`,
  sku: 'H6159',
  device_name: `Desk strip ${LEAK}`,
  mode: 'scene',
  scene_id: `lightScene:4280:3853-${LEAK}`,
  scene_name: `Sunrise ${LEAK}`,
  scene_type: 'devices.capabilities.dynamic_scene',
  scene_instance: 'lightScene',
  scene_value: `{"paramId":4280,"id":3853,"note":"${LEAK}"}`
};
const lights = () => ({ type: 'lights', params: { ...deviceParams } });
const post = () => ({ type: 'http_post', params: { url: `https://user:${LEAK}@hooks.example.org/path?token=${LEAK}`, body: `key=${LEAK}` } });
const VIEWER_LOGIN = 'realviewer';
const VIEWER_ID = '4242424242';
const users = () => ({ type: 'users', params: { allow: [VIEWER_LOGIN], deny: ['realtroll'], allow_ids: [VIEWER_ID], deny_ids: ['555000111'] } });
const manual = (actions: unknown[]): Definition => ({ version: 1, trigger: { type: 'manual', params: {} }, actions: actions as Definition['actions'] });

const fileOf = (flows: unknown[]) => JSON.stringify({ format: FLOW_FILE_FORMAT, version: FLOW_FILE_VERSION, flows });
const planned = (definition: unknown): FlowImportEntry => {
  const plan = planFlowImport(fileOf([{ ref: 'f1', name: 'one', definition }]), EMPTY);
  if (!plan.ok) throw new Error(JSON.stringify(plan.issues));
  return plan.entries[0]!;
};
const exported = (definition: Definition) => JSON.stringify(exportFlows([{ id: 'row-1', name: 'one', definition }]).file);

function throughIfAndParallel(depth: number, leaf: unknown): unknown {
  if (depth === 0) return leaf;
  const inner = throughIfAndParallel(depth - 1, leaf);
  const level = depth % 2 === 0 ? ifOf(inner) : parallelOf(inner);
  return level;
}
const ifOf = (inner: unknown) => ({
  type: 'if',
  params: { condition: users(), then: [lights(), post(), inner], else: [chat('no')] }
});
const parallelOf = (inner: unknown) => ({
  type: 'parallel',
  params: { branches: [{ weight: 1, actions: [lights(), post(), inner] }, { weight: 1, actions: [chat('b')] }] }
});

describe('the reviewed lists cover every registered type', () => {
  const sets = [
    ['trigger', triggerRegistry, TRIGGER_TYPES],
    ['condition', conditionRegistry, CONDITION_TYPES],
    ['action', actionRegistry, ACTION_TYPES]
  ] as const;

  for (const [group, registry, catalog] of sets) {
    test(`${group} types: each one is rewritten or reviewed, and the lists hold nothing stale`, () => {
      const known = new Set<string>([...SHARE_REWRITTEN[group], ...SHARE_REVIEWED[group]]);
      expect([...registry.keys()].filter((type) => !known.has(type))).toEqual([]);
      expect(catalog.map((def) => def.type).filter((type) => !known.has(type))).toEqual([]);
      expect([...known].filter((type) => !registry.has(type))).toEqual([]);
      expect(SHARE_REWRITTEN[group].filter((type) => (SHARE_REVIEWED[group] as readonly string[]).includes(type))).toEqual([]);
    });
  }
});

describe('allowed_user_id never leaves in a file', () => {
  const ID = '777000111';
  const report = (definition: unknown, restricted = false) => exportFlows([{ id: 'r', name: 'one', definition: definition as Definition, restricted }]);

  test('dropped at any depth, and the recipe still exports', () => {
    const nested = { type: 'if', params: { condition: { type: 'perm', params: { level: 'mod' }, allowed_user_id: ID }, then: [{ ...chat('hi'), params: { text: 'hi', allowed_user_id: ID } }] } };
    const { file, report: rows } = report({ version: 1, trigger: { type: 'manual', params: { allowed_user_id: ID } }, actions: [nested] });
    expect(JSON.stringify(file)).not.toContain(ID);
    expect(JSON.stringify(file)).not.toContain('allowed_user_id');
    expect(file.flows).toHaveLength(1);
    expect(rows[0]!.setup.map((note) => note.code)).toContain('users_list');
  });

  test('a restricted command leaves closed to everyone rather than open to everyone', () => {
    const definition = { version: 1, trigger: { type: 'command', params: { name: 'hug' } }, actions: [chat('hi')] };
    for (const flow of [report(definition, true), report({ ...definition, trigger: { type: 'command', params: { name: 'hug', allowed_user_id: ID } } })]) {
      const conditions = flow.file.flows[0]!.definition.conditions ?? [];
      expect(conditions).toEqual([{ type: 'users', params: { allow: [SETUP_LOGIN] } }]);
      expect(setupGaps(flow.file.flows[0]!.definition)).toHaveLength(1);
    }
  });

  test('an unrestricted command stays as it was', () => {
    const { file } = report({ version: 1, trigger: { type: 'command', params: { name: 'hug' } }, actions: [chat('hi')] });
    expect(file.flows[0]!.definition.conditions).toBeUndefined();
  });
});

describe('secrets pasted into free text', () => {
  const GH = 'ghp_0123456789abcdefABCDEF0123456789abcd';
  const OPENAI = 'sk-proj-a1B2c3D4e5F6g7H8i9J0k1L2';
  const SLACK = 'xoxb-1234567890-abcdefghijkl';
  const AWS = 'AKIAIOSFODNN7EXAMPLE';
  const TWITCH = 'oauth:q6batx0epp608isickayubi39itsck';
  const JWT_TOKEN = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U';
  const RANDOM = 'q6batx0epp608isickayubi39itsckt';
  const one = (definition: Definition, name = 'one') => {
    const out = exportFlows([{ id: 'r', name, definition }]);
    return { text: JSON.stringify(out.file), report: out.report[0]!, flow: out.file.flows[0] };
  };
  const leaks = [
    ['GitHub', GH, 'known_token'],
    ['OpenAI', OPENAI, 'known_token'],
    ['Slack', SLACK, 'known_token'],
    ['AWS', AWS, 'known_token'],
    ['Twitch', TWITCH, 'known_token'],
    ['Bearer', `Bearer ${GH.slice(4)}`, 'known_token'],
    ['JWT', JWT_TOKEN, 'jwt'],
    ['random run', RANDOM, 'high_entropy']
  ] as const;

  for (const [label, secret, kind] of leaks) {
    test(`${label}: the substring goes, the sentence stays, the report names the step`, () => {
      const { text, report: row, flow } = one(manual([chat('first'), chat(`my key is ${secret} ok`)]));
      expect(text).not.toContain(secret);
      expect(flow!.definition.actions![1]!.params!.text).toBe('my key is  ok');
      expect(row.scrubbed).toEqual([{ path: 'actions[1].params.text', type: 'chat', kind }]);
      expect(JSON.stringify(row)).not.toContain(secret);
    });
  }

  const fields: [string, unknown][] = [
    ['reply', { type: 'reply', params: { text: GH } }],
    ['var value', { type: 'var_set', params: { scope: 'channel', name: 'k', value: `x ${GH}` } }],
    ['text step', { type: 'text', params: { op: 'upper', input: `x ${GH}`, arg1: '', arg2: '', save_as: 'y' } }],
    ['overlay text', { type: 'overlay_show', params: { kind: 'text', content: `x ${GH}`, seconds: 5, position: 'top' } }],
    ['nested body', { type: 'if', params: { condition: { type: 'compare', params: { left: `x ${GH}`, op: 'eq', right: '1' } }, then: [{ type: 'loop', params: { times: '2', actions: [chat(`x ${GH}`)] } }], else: [] } }]
  ];

  for (const [label, action] of fields) {
    test(`${label} is scanned`, () => {
      const { text, report: row } = one(manual([action]));
      expect(text).not.toContain(GH);
      expect(row.scrubbed.length).toBeGreaterThan(0);
      expect(row.issues).toEqual([]);
    });
  }

  test('a field that was nothing but the key leaves off and awaiting setup, so the recipe still exports', () => {
    const { report: row, flow } = one(manual([chat(GH)]));
    expect(row.issues).toEqual([]);
    expect(flow!.definition.actions![0]).toMatchObject({ enabled: false, params: { text: 'setup-required' } });
    expect(row.setup.map((note) => note.code)).toContain('secret_removed');
  });

  test('the recipe name is scanned', () => {
    const { text, report: row } = one(manual([chat('hi')]), `Mine ${GH}`);
    expect(text).not.toContain(GH);
    expect(row.scrubbed[0]).toMatchObject({ path: 'name', kind: 'known_token' });
  });

  const urls = [
    ['userinfo', 'see https://me:hunter2@example.org/a ok', 'see https://example.org/a ok'],
    ['token param', 'https://example.org/a?token=hunter2&page=2', 'https://example.org/a?token=&page=2'],
    ['api key param', 'https://example.org/a?page=2&api_key=hunter2', 'https://example.org/a?page=2&api_key='],
    ['fragment token', 'https://example.org/cb#access_token=hunter2', 'https://example.org/cb#access_token='],
    ['stream url', 'rtmp://me:hunter2@live.example.org/app', 'rtmp://live.example.org/app']
  ] as const;

  for (const [label, input, output] of urls) {
    test(`URL ${label} is cleaned`, () => {
      const { flow, report: row } = one(manual([chat(input)]));
      expect(flow!.definition.actions![0]!.params!.text).toBe(output);
      expect(row.scrubbed[0]).toMatchObject({ kind: 'url_credentials' });
    });
  }

  const harmless = [
    'gg wp everyone, thanks for the raid!',
    'hahahahahahahahahahahahahahahahahahahahahaha',
    'LULLULLULLULLULLULLULLULLULLULLULLUL',
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    'The Bearer responsibilities are many',
    'oauth: please log in again',
    'sk-learn and task-management are fine',
    'https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=30s',
    'https://example.org/my-very-long-article-title-about-streaming-setups-in-2026',
    'https://example.org/a?page=2&sort=new&monkey=banana',
    'https://example.org/a?token={token}&user={user}',
    'https://{user}@example.org/a',
    'Your token is {token}, {user}. Bearer {token}',
    'order 550e8400-e29b-41d4-a716-446655440000 shipped',
    'ThisIsMyVeryLongStreamTitleForTheYear2026Edition',
    'pneumonoultramicroscopicsilicovolcanoconiosis',
    '{choice:one|two|three} {random.1-100} {var:my_long_variable_name_here}',
    '1234567890123456789012345678901234567890'
  ];

  for (const line of harmless) {
    test(`left alone: ${line}`, () => {
      const { flow, report: row } = one(manual([chat(line)]));
      expect(flow!.definition.actions![0]!.params!.text).toBe(line);
      expect(row.scrubbed).toEqual([]);
    });
  }

  test('placeholders from an earlier export survive another export', () => {
    const definition = manual([{ type: 'http_post', enabled: false, params: { url: SETUP_URL } }]);
    expect(one(definition).report.scrubbed).toEqual([]);
  });
});

describe('setup gaps', () => {
  test('every neutralized step and trigger counts until filled, and a finished recipe has none', () => {
    const imported = planned({ version: 1, trigger: { type: 'reward', params: { reward_id: 'real' } }, actions: [post(), lights(), chat('hi')] });
    const paths = setupGaps(imported.definition!).map((issue) => issue.path);
    expect(paths).toEqual(['trigger', 'actions[0]', 'actions[1]']);
    expect(setupGaps(manual([chat('hi'), { type: 'http_post', params: { url: 'https://hooks.example.org/a' } }]))).toEqual([]);
  });

  test('an allow list nobody is on yet draws as a user check and lowers back to the same list', () => {
    const definition: Definition = { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, conditions: [{ type: 'users', params: { allow: [SETUP_LOGIN] } }], actions: [chat('hi') as never] };
    const drawn = definitionToFlow(definition, { id: 'a', name: 'a', enabled: false });
    if (!drawn.ok) throw new Error(JSON.stringify(drawn.issues));
    const lowered = flowToDefinition(drawn.value);
    if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
    expect(lowered.value.conditions).toEqual([{ type: 'users', params: { allow: [SETUP_LOGIN] } }]);
  });
});

describe('a type nobody reviewed exports and imports without its params', () => {
  afterEach(() => {
    actionRegistry.delete('webhook');
    conditionRegistry.delete('geo');
    triggerRegistry.delete('socket');
  });
  const open = () => register<Record<string, unknown>>({ url: 'string', token: 'string' }, () => {});

  test('action', () => {
    actionRegistry.set('webhook', open());
    const definition = manual([{ type: 'webhook', params: { url: `https://x.example.org/${LEAK}`, token: LEAK } }]);
    expect(exported(definition)).not.toContain(LEAK);
  });

  test('condition', () => {
    conditionRegistry.set('geo', open());
    const definition: Definition = { ...manual([chat('hi')]), conditions: [{ type: 'geo', params: { url: LEAK, token: LEAK } }] };
    expect(exported(definition)).not.toContain(LEAK);
  });

  test('trigger', () => {
    triggerRegistry.set('socket', open());
    const definition: Definition = { version: 1, trigger: { type: 'socket', params: { url: LEAK, token: LEAK } }, actions: [chat('hi')] };
    expect(exported(definition)).not.toContain(LEAK);
  });

  test('import', () => {
    actionRegistry.set('webhook', open());
    const entry = planned(manual([{ type: 'webhook', params: { url: `https://x.example.org/${LEAK}`, token: LEAK } }]));
    expect(JSON.stringify(entry.definition ?? null)).not.toContain(LEAK);
  });
});

describe('what an exported recipe carries', () => {
  test('device, scene, url, body and user lists are replaced, whatever the row holds', () => {
    const definition: Definition = {
      version: 1,
      trigger: { type: 'reward', params: { reward_id: `rw-${LEAK}` } },
      conditions: [users() as never],
      actions: [lights(), post(), chat('hello {user}')] as never
    };
    const text = exported(definition);
    expect(text).not.toContain(LEAK);
    for (const personal of ['555000111', VIEWER_ID, VIEWER_LOGIN, 'realtroll']) expect(text).not.toContain(personal);
    expect(text).not.toContain('hooks.example.org');
    expect(text).not.toContain('AB:CD:EF');
    expect(text).toContain('hello {user}');
  });

  test('a row with fields the editor never writes is left out, not trimmed', () => {
    const rows = [{ id: 'a', name: 'x', definition: { ...manual([chat('hi')]), api_key: LEAK } as Definition }];
    const { file, report } = exportFlows(rows);
    expect(JSON.stringify(file)).not.toContain(LEAK);
    expect(report[0]!.issues.length + file.flows.length).toBeGreaterThan(0);
  });

  test('extra fields on a step or its params stop that recipe from exporting', () => {
    const rows = [
      { id: 'a', name: 'x', definition: manual([{ ...lights(), key: LEAK }]) },
      { id: 'b', name: 'y', definition: manual([{ type: 'http_post', params: { url: 'https://example.org/', body: '', headers: LEAK } }]) },
      { id: 'c', name: 'z', definition: manual([{ type: 'chat', params: { text: 'hi', token: LEAK } }]) }
    ];
    const { file } = exportFlows(rows);
    expect(JSON.stringify(file)).not.toContain(LEAK);
  });
});

describe('a hostile file', () => {
  test('a real looking Govee step, url and user ids never survive import', () => {
    const entry = planned({
      version: 1,
      trigger: { type: 'reward', params: { reward_id: `rw-${LEAK}` } },
      conditions: [users()],
      actions: [lights(), post(), chat('hello')]
    });
    expect(entry.status).toBe('ready');
    const text = JSON.stringify(entry.definition);
    expect(text).not.toContain(LEAK);
    expect(text).not.toContain(VIEWER_ID);
    expect(text).not.toContain(VIEWER_LOGIN);
    expect(text).not.toContain('hooks.example.org');
    expect(entry.definition!.actions!.filter((a) => a.type !== 'chat').every((a) => a.enabled === false)).toBe(true);
  });

  test('an allow list kept only as ids stays an allow list, with nobody on it', () => {
    const entry = planned({
      version: 1,
      trigger: { type: 'manual', params: {} },
      conditions: [{ type: 'users', params: { allow_ids: ['555000111'] } }],
      actions: [chat('hi')]
    });
    expect(entry.definition!.conditions).toEqual([{ type: 'users', params: { allow: [SETUP_LOGIN] } }]);
  });

  test('a deny list kept as ids is emptied', () => {
    const entry = planned({
      version: 1,
      trigger: { type: 'manual', params: {} },
      conditions: [{ type: 'users', params: { deny_ids: ['555000111'] } }],
      actions: [chat('hi')]
    });
    expect(entry.definition!.conditions).toEqual([{ type: 'users', params: { deny: [SETUP_LOGIN] } }]);
  });

  test('an extra field planted on any object of a full recipe never reaches the plan', () => {
    const full = (): Record<string, unknown> => ({
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.cheer', filters: { amount: '100' } } },
      conditions: [{ type: 'perm', params: { level: 'sub' } }, users()],
      actions: [
        lights(),
        post(),
        ifOf(parallelOf(lights())),
        { type: 'random_pick', params: { branches: [{ weight: 1, actions: [lights()] }, { weight: 2, actions: [post()] }] } }
      ]
    });
    const paths: (string | number)[][] = [];
    const collect = (node: unknown, path: (string | number)[]) => {
      if (typeof node !== 'object' || node === null) return;
      if (!Array.isArray(node)) paths.push(path);
      for (const [key, child] of Object.entries(node)) collect(child, [...path, Array.isArray(node) ? Number(key) : key]);
    };
    collect(full(), []);
    expect(paths.length).toBeGreaterThan(20);
    for (const path of paths) {
      const definition = full();
      let at: any = definition;
      for (const key of path) at = at[key];
      at.leak = LEAK;
      const entry = planned(definition);
      expect(JSON.stringify(entry.definition ?? null), path.join('.')).not.toContain(LEAK);
    }
  });

  test('extra fields on the file and on a recipe are refused', () => {
    const top = planFlowImport(JSON.stringify({ format: FLOW_FILE_FORMAT, version: FLOW_FILE_VERSION, flows: [], user_id: LEAK }), EMPTY);
    expect(top.ok).toBe(false);
    const row = planFlowImport(fileOf([{ ref: 'f1', name: 'x', definition: manual([chat('hi')]), user_id: LEAK, enabled: true }]), EMPTY);
    expect(row.ok && row.entries[0]!.status).toBe('invalid');
  });
});

function expectNeutral(definition: Definition, depth: number): void {
  const text = JSON.stringify(definition);
  for (const leak of [LEAK, 'hooks.example.org', 'AB:CD:EF', VIEWER_LOGIN, VIEWER_ID]) expect(text).not.toContain(leak);
  const walk = (actions: any[]): any[] =>
    actions.flatMap((a) => [a, ...walk(a.params?.then ?? []), ...walk(a.params?.else ?? []), ...(a.params?.branches ?? []).flatMap((b: any) => walk(b.actions))]);
  const steps = walk(definition.actions ?? []);
  const external = steps.filter((a) => a.type === 'lights' || a.type === 'http_post');
  expect(external.length).toBe(2 * depth);
  expect(external.every((a) => a.enabled === false)).toBe(true);
  expect(steps.filter((a) => a.type === 'if').every((a) => JSON.stringify(a.params.condition).includes(SETUP_LOGIN))).toBe(true);
}

describe('recipes v2 external targets never leave, at any depth', () => {
  const v2Leaves = () => [
    { type: 'http_get', params: { url: `https://user:${LEAK}@api.example.org/a?t=${LEAK}`, json_path: ['data', 'value'], save_as: 'answer' } },
    { type: 'http_post', params: { url: `https://hooks.example.org/${LEAK}`, body: `key=${LEAK}`, save_as: 'reply' } },
    { type: 'reward_update', params: { reward_id: `reward-${LEAK}`, enabled: false, cost: null, reset_on_stream_end: true } },
    { type: 'overlay_show', params: { kind: 'image', content: `https://cdn.example.org/${LEAK}.png`, seconds: 5, position: 'top' } },
    { type: 'overlay_show', params: { kind: 'video', content: `https://cdn.example.org/${LEAK}.mp4`, seconds: 5, position: 'top' } },
    { type: 'overlay_sound', params: { url: `https://cdn.example.org/${LEAK}.mp3`, volume: 40 } }
  ];

  const inLoop = (inner: unknown[]) => ({ type: 'loop', params: { times: '2', actions: inner } });
  const inOk = (inner: unknown[]) => ({ type: 'http_get', params: { url: `https://api.example.org/${LEAK}`, on_ok: inner } });
  const inFailure = (inner: unknown[]) => ({ type: 'http_post', params: { url: `https://hooks.example.org/${LEAK}`, on_failure: inner } });
  const inIf = (inner: unknown[]) => ({ type: 'if', params: { condition: { type: 'live', params: { state: 'live' } }, then: inner } });
  const inFork = (inner: unknown[]) => ({ type: 'parallel', params: { branches: [{ weight: 1, actions: inner }, { weight: 1, actions: [chat('b')] }] } });

  const wrappers = { loop: inLoop, on_ok: inOk, on_failure: inFailure, if: inIf, parallel: inFork };

  function nested(depth: number): unknown[] {
    const chain = [inLoop, inOk, inFailure, inIf, inFork];
    let list: unknown[] = v2Leaves();
    for (let level = 0; level < depth; level++) list = [chain[level % chain.length]!(list)];
    return list;
  }

  test.each(Object.entries(wrappers))('directly inside %s, export and import both drop every target', (_name, wrap) => {
    const definition = manual([wrap(v2Leaves())]);
    expect(exported(definition)).not.toContain(LEAK);
    const entry = planned(definition);
    expect(entry.status).toBe('ready');
    expect(JSON.stringify(entry.definition)).not.toContain(LEAK);
  });

  test.each([1, 2, 3, 5, 8, 12])('%i containers deep', (depth) => {
    const definition = manual(nested(depth));
    const { file, report } = exportFlows([{ id: 'row-1', name: 'one', definition }]);
    expect(report[0]!.issues).toEqual([]);
    expect(JSON.stringify(file)).not.toContain(LEAK);
    const entry = planned(definition);
    expect(entry.status).toBe('ready');
    expect(JSON.stringify(entry.definition)).not.toContain(LEAK);
  });

  test('a neutralised API node keeps its paths, save_as and json_path, and turns off', () => {
    const entry = planned(manual([{ type: 'http_get', params: { url: `https://api.example.org/${LEAK}`, json_path: ['a', 'b'], save_as: 'answer', on_ok: [chat('good')], on_failure: [chat('bad')] } }]));
    expect(entry.definition!.actions).toEqual([
      { type: 'http_get', enabled: false, params: { url: SETUP_URL, json_path: ['a', 'b'], save_as: 'answer', on_ok: [chat('good')], on_failure: [chat('bad')] } }
    ]);
    expect(entry.setup.map((note) => note.code)).toEqual(['http_target']);
  });

  test('a post keeps its paths and loses its body', () => {
    const entry = planned(manual([{ type: 'http_post', params: { url: `https://hooks.example.org/${LEAK}`, body: `x=${LEAK}`, on_failure: [chat('bad')] } }]));
    expect(entry.definition!.actions).toEqual([{ type: 'http_post', enabled: false, params: { url: SETUP_URL, on_failure: [chat('bad')] } }]);
  });

  test('text overlays and the listed kept fields stay as written', () => {
    const steps = [
      { type: 'overlay_show', params: { kind: 'text', content: 'hello {user}', seconds: 5, position: 'top' } },
      { type: 'var_set', params: { scope: 'channel', name: 'score', value: '1' } },
      { type: 'var_add', params: { scope: 'viewer', name: 'score', amount: 2 } },
      { type: 'var_delete', params: { scope: 'channel', name: 'score' } },
      { type: 'text', params: { op: 'upper', input: '{user}', arg1: '', arg2: '', save_as: 'loud' } },
      { type: 'math', params: { expression: '1+1', save_as: 'sum' } },
      { type: 'loop', params: { times: '3', actions: [chat('a')] } }
    ];
    const entry = planned({ ...manual(steps), queue: 'sounds' });
    expect(entry.definition!.actions).toEqual(steps as NonNullable<Definition['actions']>);
    expect(entry.definition!.queue).toBe('sounds');
    expect(entry.setup).toEqual([]);
  });

  test('the import view counts the setup work and the imported steps stay valid', () => {
    const entry = planned(manual(v2Leaves()));
    expect(entry.setup.map((note) => note.code)).toEqual(['http_target', 'http_target', 'reward_target', 'overlay_target', 'overlay_target', 'overlay_target']);
    expect((entry.definition!.actions ?? []).every((action) => action.enabled === false)).toBe(true);
  });
});

describe('nesting never hides a step from the neutraliser', () => {
  test.each([1, 2, 3, 6])('lights and http_post %i levels deep through if and parallel', (depth) => {
    const definition = manual([throughIfAndParallel(depth, chat('end'))]);
    const entry = planned(definition);
    expect(entry.status).toBe('ready');
    expectNeutral(entry.definition!, depth);
  });

  test.each([1, 3, 6])('the same holds on export, %i levels deep', (depth) => {
    const { file, report } = exportFlows([{ id: 'row-1', name: 'one', definition: manual([throughIfAndParallel(depth, chat('end'))]) }]);
    expect(report[0]!.issues).toEqual([]);
    expectNeutral(file.flows[0]!.definition, depth);
  });
});
