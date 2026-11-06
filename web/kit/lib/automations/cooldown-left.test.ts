// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { definitionToFlow, flowToDefinition } from './flow-graph';
import { classifyFlow } from './flow-layer';
import { exportFlows, FLOW_FILE_FORMAT, FLOW_FILE_VERSION, planFlowImport } from './flow-share';
import type { Definition } from './types';
import { leftCommandNames, validateDefinition } from './validate';

const cooldown = { global_s: 60, left_command: '!timeleft', left_reply: '{cooldown:left} more, {user}', ready_reply: 'go {user}' };
const recipe = (params: Record<string, unknown> = cooldown): Definition => ({
  version: 1,
  trigger: { type: 'command', params: { name: '!hug' } },
  conditions: [{ type: 'cooldown', params }],
  actions: [{ type: 'chat', params: { text: 'hi' } }]
});
const EMPTY = { count: 0, names: [], commands: [], alerts: [] };
const fileOf = (definition: unknown) => JSON.stringify({ format: FLOW_FILE_FORMAT, version: FLOW_FILE_VERSION, flows: [{ ref: 'f1', name: 'one', definition }] });

describe('the time left fields', () => {
  const throughBuilder = (definition: Definition): Definition => {
    const drawn = definitionToFlow(definition, { id: 'f', name: 'one', enabled: true });
    if (!drawn.ok) throw new Error(JSON.stringify(drawn.issues));
    const lowered = flowToDefinition(drawn.value);
    if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
    return lowered.value;
  };

  test('survive the builder graph', () => {
    expect(throughBuilder(recipe()).conditions![0]!.params).toEqual(cooldown);
  });

  test('stay out of the definition when empty', () => {
    expect(throughBuilder(recipe({ user_s: 60 })).conditions![0]!.params).toEqual({ user_s: 60 });
  });

  test('keep a recipe that would otherwise read as a plain command', () => {
    expect(classifyFlow(recipe({ global_s: 60 })).layer).toBe('command');
    expect(classifyFlow(recipe()).layer).toBe('automation');
    expect(classifyFlow(recipe({ global_s: 60, left_reply: 'x' })).layer).toBe('automation');
  });

  test('are found in an if at any depth', () => {
    const nested = {
      ...recipe({ global_s: 60 }),
      actions: [
        {
          type: 'loop',
          params: {
            times: '2',
            actions: [{ type: 'if', params: { condition: { type: 'cooldown', params: { user_s: 5, left_command: '!deep' } }, then: [], else: [] } }]
          }
        }
      ]
    } as Definition;

    expect(leftCommandNames(nested)).toEqual(['!deep']);
    expect(validateDefinition(nested)).toEqual([]);
  });

  test('refuse a clash with the recipe itself', () => {
    expect(validateDefinition(recipe({ global_s: 60, left_command: 'HUG' }))).toEqual([{ path: 'left_command', code: 'command_exists' }]);
  });

  test('refuse a second word, as a start-match trigger does', () => {
    expect(validateDefinition(recipe({ global_s: 60, left_command: '!time left' }))).toEqual([{ path: 'conditions[0].params.left_command', code: 'invalid_format' }]);
  });
});

describe('a shared recipe', () => {
  test('keeps its left command and scrubs secrets from the replies', () => {
    const leak = 'ghp_abcdefghijklmnopqrstuvwxyz0123456789';
    const { file } = exportFlows([{ id: 'a', name: 'one', definition: recipe({ ...cooldown, left_reply: `key ${leak} {cooldown:left}` }) }]);
    const params = file.flows[0]!.definition.conditions![0]!.params!;

    expect(params.left_command).toBe('!timeleft');
    expect(JSON.stringify(params)).not.toContain(leak);
    expect(String(params.left_reply)).toContain('{cooldown:left}');
  });

  test('refuses an import whose left command is taken', () => {
    const plan = planFlowImport(fileOf(recipe()), { ...EMPTY, commands: ['timeleft'] });
    if (!plan.ok) throw new Error(JSON.stringify(plan.issues));

    expect(plan.entries[0]!.status).toBe('conflict');
    expect(plan.entries[0]!.issues).toEqual([{ path: 'left_command', code: 'command_exists' }]);
  });

  test('imports when the name is free', () => {
    const plan = planFlowImport(fileOf(recipe()), EMPTY);
    if (!plan.ok) throw new Error(JSON.stringify(plan.issues));

    expect(plan.entries[0]!.issues).toEqual([]);
    expect(plan.entries[0]!.definition!.conditions![0]!.params!.left_command).toBe('!timeleft');
  });
});

describe('recipe names', () => {
  const named = (name: string): Definition => ({ version: 1, trigger: { type: 'manual', params: {} }, actions: [{ type: 'var_set', params: { scope: 'channel', name, value: 'x' } }] });

  test.each(['points', 'wins2', 'Deaths'])('%s is one simple word', (name) => {
    expect(validateDefinition(named(name))).toEqual([]);
  });

  test.each(['my_points', 'my:points', 'my-points', 'my points'])('%s asks for one simple word', (name) => {
    expect(validateDefinition(named(name))).toEqual([{ path: 'actions[0].params.name', code: 'simple_word' }]);
  });
});
