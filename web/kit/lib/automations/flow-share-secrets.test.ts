// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, test } from 'bun:test';
import { actionRegistry, conditionRegistry, register, triggerRegistry } from './engine';
import { exportFlows, FLOW_FILE_FORMAT, FLOW_FILE_VERSION, planFlowImport, SETUP_LOGIN, SHARE_REVIEWED, SHARE_REWRITTEN, type FlowImportEntry } from './flow-share';
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
