// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { definitionToFlow, flowToDefinition } from './flow-graph';
import { exportFlows, FLOW_FILE_FORMAT, FLOW_FILE_VERSION, planFlowImport } from './flow-share';
import { leftCommandNames, ownedCountdownNames, validateDefinition } from './validate';
import type { Definition } from './types';

const start = { type: 'countdown_start', params: { name: 'Subathon', seconds: 3600, left_command: '!left', left_reply: '{countdown:left} left', ready_reply: 'Done' } };
const recipe: Definition = { version: 1, trigger: { type: 'command', params: { name: '!start' } }, conditions: [{ type: 'perm', params: { level: 'mod' } }], actions: [start] };
const empty = { count: 0, names: [], commands: [], alerts: [] };

function throughBuilder(definition: Definition): Definition {
  const graph = definitionToFlow(definition, { id: 'f', name: 'one', enabled: true });
  if (!graph.ok) throw new Error(JSON.stringify(graph.issues));
  const result = flowToDefinition(graph.value);
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

describe('countdowns', () => {
  test('start survives the builder', () => {
    const result = throughBuilder(recipe);
    expect(result.trigger.type).toBe(recipe.trigger.type);
    expect(result.actions?.[0]?.params).toEqual({ ...start.params, name: 'subathon' });
  });
  test('finished trigger survives the builder', () => {
    const definition: Definition = { version: 1, trigger: { type: 'countdown', params: { name: 'subathon' } }, actions: [{ type: 'chat', params: { text: 'Done!' } }] };
    expect(throughBuilder(definition).trigger).toEqual(definition.trigger);
  });
  test('finds nested starts and detects collisions with legacy cooldown commands', () => {
    const def: Definition = { ...recipe, conditions: [{ type: 'perm', params: { level: 'mod' } }, { type: 'cooldown', params: { global_s: 5, left_command: '!LEFT' } }], actions: [{ type: 'loop', params: { times: '2', actions: [start] } }] };
    expect(ownedCountdownNames(def)).toEqual(['subathon']);
    expect(leftCommandNames(def)).toEqual(['!LEFT', '!left']);
    expect(validateDefinition(def)).toContainEqual({ path: 'left_command', code: 'command_exists' });
  });
  test('shares countdowns and checks command claims during import', () => {
    const { file } = exportFlows([{ id: 'a', name: 'one', definition: recipe }]);
    expect(file.flows[0]?.definition.actions?.[0]?.params).toEqual(start.params);
    const plan = planFlowImport(JSON.stringify({ format: FLOW_FILE_FORMAT, version: FLOW_FILE_VERSION, flows: file.flows }), { ...empty, commands: ['left'] });
    if (!plan.ok) throw new Error(JSON.stringify(plan.issues));
    expect(plan.entries[0]?.issues).toContainEqual({ path: 'left_command', code: 'command_exists' });
  });
  test('scrubs secrets in countdown replies during export', () => {
    const secret = 'ghp_' + 'test'.repeat(9);
    const def = { ...recipe, actions: [{ ...start, params: { ...start.params, left_reply: `key ${secret} {countdown:left}` } }] };
    const { file } = exportFlows([{ id: 'a', name: 'one', definition: def }]);
    expect(JSON.stringify(file)).not.toContain(secret);
    expect(JSON.stringify(file)).toContain('{countdown:left}');
  });
  test('rejects invalid names, duration and amount rates', () => {
    expect(validateDefinition({ ...recipe, actions: [{ type: 'countdown_start', params: { name: 'bad_name', seconds: 2592001 } }] })).toEqual([
      { path: 'actions[0].params.name', code: 'simple_word' }, { path: 'actions[0].params.seconds', code: 'out_of_range' }
    ]);
    expect(validateDefinition({ ...recipe, actions: [{ type: 'countdown_add', params: { name: 'timer', seconds: 1, per: 10 } }] })).toContainEqual({ path: 'actions[0].params.per', code: 'trigger_mismatch' });
  });
});
