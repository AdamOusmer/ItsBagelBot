// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { definitionToFlow, flowToDefinition, type Flow } from './flow-graph';
import { exportFlows, SETUP_LOGIN } from './flow-share';
import type { Action, Definition } from './types';
import { validateDefinition } from './validate';

const manual = (actions: Action[]): Definition => ({ version: 1, trigger: { type: 'manual', params: {} }, actions });

test('stream steps keep event templates and separate options across a save', () => {
  const flow: Flow = {
    id: 'f', name: 'f', enabled: true,
    nodes: [
      { id: 't', type: 'twitch.cheer', x: 0, y: 0, config: { minBits: 1 } },
      { id: 'p', type: 'reply.poll', x: 0, y: 0, config: { title: '{bits} bits', choices: 'Yes {user}\nNo', seconds: 60 } }
    ],
    edges: [{ id: 'e', from: 't', port: 'next', to: 'p' }]
  };
  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  expect(validateDefinition(lowered.value)).toEqual([]);
  expect(lowered.value.actions[0]?.params).toEqual({ title: '{event.amount} bits', choices: ['Yes {user}', 'No'], seconds: 60 });
  const reopened = definitionToFlow(lowered.value, flow);
  if (!reopened.ok) throw new Error(JSON.stringify(reopened.issues));
  expect(reopened.value.nodes.find((node) => node.type === 'reply.poll')?.config).toEqual(flow.nodes[1]!.config);
});

test('opening an option containing a newline refuses a lossy conversion', () => {
  const definition: Definition = {
    version: 1, trigger: { type: 'event', params: { kind: 'stream.online' } },
    actions: [{ type: 'poll', params: { title: 'Pick', choices: ['One\nTwo', 'Three'], seconds: 60 } }]
  };
  const result = definitionToFlow(definition, { id: 'f', name: 'f', enabled: true });
  expect(result.ok).toBe(false);
  if (!result.ok) expect(result.issues).toContainEqual({ path: 'actions[0]', code: 'unsupported_action' });
});

test.each(['raid', 'mod', 'vip', 'whisper'])('sharing %s removes a literal account and disables the action', (type) => {
  const extra = type === 'whisper' ? { text: 'Hello' } : { on: true };
  const params = type === 'raid' ? { login: 'specificviewer' } : { login: 'specificviewer', ...extra };
  const exported = exportFlows([{ id: 'one', name: 'one', definition: manual([{ type, params }]) }]);
  const serialized = JSON.stringify(exported.file);
  expect(serialized).not.toContain('specificviewer');
  expect(serialized).toContain(SETUP_LOGIN);
  const action = exported.file.flows[0]!.definition.actions[0]!;
  expect(action.enabled).toBe(false);
});

test('sharing a stream action preserves a dynamic viewer target', () => {
  const definition = manual([{ type: 'vip', params: { login: '{user}', on: true } }]);
  const exported = exportFlows([{ id: 'one', name: 'one', definition }]);
  expect(exported.file.flows[0]!.definition.actions[0]).toEqual(definition.actions[0]);
});

test('unban resolution requires the matching event even inside a branch', () => {
  const definition: Definition = {
    version: 1, trigger: { type: 'event', params: { kind: 'stream.online' } },
    actions: [{ type: 'if', params: { condition: { type: 'chance', params: { percent: 100 } }, then: [{ type: 'unban_resolve', params: { status: 'approve' } }] } }]
  };
  expect(validateDefinition(definition)).toContainEqual({ path: 'actions[0].params.then[0].type', code: 'trigger_mismatch' });
  definition.trigger.params = { kind: 'channel.unban_request.create' };
  expect(validateDefinition(definition)).toEqual([]);
});
