// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, locateIssues, validateDefinition } from '@bagel/kit/automations';
import { fieldIssues, fieldRequired, missingField } from './issues';
import { newFlow, type FlowNode } from './model';

const nodeOf = (type: FlowNode['type'], config: FlowNode['config'] = {}): FlowNode => ({ id: 'step', type, config, x: 0, y: 0 });

const mappings = [
  ['reply.chat', 'params.text', 'message'], ['reply.wait', 'params.ms', 'seconds'],
  ['reply.math', 'params.save_as', 'saveAs'], ['reply.http_get', 'params.json_path[0]', 'jsonPath'],
  ['reply.http_post', 'params.save_as', 'saveAs'], ['reply.counter', 'params.name', 'counter'],
  ['cond.cooldown', 'params.global_s', 'seconds'], ['cond.cooldown', 'params.user_s', 'seconds'],
  ['cond.cooldown', 'params.left_command', 'leftCommand'], ['cond.cooldown', 'params.left_reply', 'leftReply'],
  ['reply.countdown_start', 'params.ready_reply', 'readyReply'],
  ['cond.user', 'params.allow_ids[0]', 'login'], ['cond.user', 'params.allow[0]', 'login'], ['cond.user', 'userId', 'login'],
  ['twitch.chat.command', 'params.name', 'trigger'], ['twitch.chat.word', 'params.name', 'word'],
  ['twitch.sub', 'params.filters.is_gift', 'variant'], ['twitch.cheer', 'params.filters.amount', 'minBits'],
  ['twitch.raid', 'params.filters.viewers', 'minViewers'], ['twitch.reward', 'params.reward_id', 'reward'],
  ['twitch.reward', 'params.title', 'reward'], ['twitch.reward', 'params.cost', 'reward'],
  ['twitch.reward', 'params.color', 'reward'], ['twitch.reward', 'params.cooldown_s', 'reward'],
  ['twitch.reward', 'params.on_redeem', 'reward'], ['twitch.reward', 'params.input_required', 'reward'],
  ['cond.cooldown', 'params', 'seconds'], ['cond.user', 'params', 'login'],
  ['reply.lights', 'params.sku', 'device'], ['reply.lights', 'params.extra_lights[0]', 'device'],
  ['reply.lights', 'params.device_name', 'device'], ['reply.lights', 'params.scene_value', 'scene'],
  ['reply.lights', 'params.scene_type', 'scene'], ['reply.lights', 'params.scene_instance', 'scene'],
  ['reply.lights', 'params.scene_id', 'scene'], ['reply.lights', 'params.scene_name', 'scene'],
  ['reply.lights', 'params.any_time', 'liveOnly'],
  ['reply.lights', 'params.kelvin', 'kelvin'], ['reply.lights', 'params.brightness', 'brightness'],
  ['reply.lights', 'params.hold_s', 'holdS'],
  ['reply.reward_update', 'params.reward_id', 'reward'], ['reply.reward_update', 'params.enabled', 'setEnabled'],
  ['reply.reward_update', 'params.reset_on_stream_end', 'reset'],
  ['reply.shoutout', 'params.login', 'target'], ['reply.timeout', 'params.login', 'target'],
  ['reply.whisper', 'params.login', 'target'], ['reply.raid', 'params.login', 'target'],
  ['reply.mod', 'params.login', 'target'], ['reply.vip', 'params.login', 'target'],
  ['reply.poll', 'params.choices[1]', 'choices'], ['reply.prediction', 'params.outcomes[0]', 'outcomes'],
  ['reply.chat', 'message.bits', 'message'], ['reply.math', 'config.expression', 'expression']
];

test.each(mappings)('%s maps %s to %s', (type, path, fieldKey) => {
  expect(fieldIssues([{ path: `nodes.step.${path}`, code: 'required' }], [nodeOf(type)]))
    .toEqual([{ path: `nodes.step.${path}`, code: 'required', nodeId: 'step', fieldKey }]);
});

test.each([
  ['replace', 'find', 'replaceWith'], ['substring', 'start', 'count'], ['split_pick', 'separator', 'pick'], ['contains', 'needle', undefined]
])('text %s maps its operation arguments', (op, first, second) => {
  const issues = ['arg1', 'arg2'].map((arg) => ({ path: `nodes.step.params.${arg}`, code: 'too_long' }));
  expect(fieldIssues(issues, [nodeOf('reply.text', { op })]).map((issue) => issue.fieldKey)).toEqual([first, second]);
});

test.each([
  { path: 'trigger.params.name', field: 'trigger', id: 'step' },
  { path: 'nodes.step', field: undefined, id: 'step' },
  { path: 'nodes.missing.params.text', field: undefined, id: undefined },
  { path: 'name', field: undefined, id: undefined }
])('locates $path without inventing a field', ({ path, field, id }) => {
  const [issue] = fieldIssues([{ path, code: 'required' }], [nodeOf('twitch.chat.command')]);
  expect({ id: issue?.nodeId, field: issue?.fieldKey } as { id?: string; field?: string }).toEqual({ id, field });
});

test.each([
  ['reply.chat', 'message', true], ['reply.wait', 'seconds', true], ['reply.math', 'saveAs', true],
  ['reply.http_get', 'saveAs', false], ['reply.text', 'input', false], ['reply.text', 'saveAs', true],
  ['cond.user', 'login', true], ['cond.cooldown', 'seconds', true], ['cond.cooldown', 'name', false],
  ['twitch.chat.command', 'trigger', true], ['twitch.reward', 'reward', true], ['reply.shoutout', 'target', true],
  ['reply.raid', 'target', true], ['reply.mod', 'target', false], ['reply.vip', 'target', false],
  ['reply.whisper', 'target', true], ['reply.timeout', 'target', false],
  ['reply.countdown_start', 'name', true], ['reply.countdown_start', 'seconds', true], ['reply.countdown_start', 'leftReply', false]
] as const)('%s marks %s required: %s', (type, key, expected) => {
  expect(fieldRequired(nodeOf(type), key)).toBe(expected);
});

test.each(['reply.chat', 'reply.wait', 'reply.math'])('%s attaches real validation failures to visible fields', (type) => {
  const flow = newFlow('Field errors');
  flow.nodes = [{ ...nodeOf('twitch.follow'), id: 'trigger' }, nodeOf(type)];
  flow.edges = [{ id: 'edge', from: 'trigger', port: 'next', to: 'step' }];
  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  const issues = fieldIssues(locateIssues(flow, validateDefinition(lowered.value)), flow.nodes);
  expect(issues.map(({ nodeId, fieldKey }) => ({ nodeId, fieldKey }))).toEqual(
    (type === 'reply.math' ? ['expression', 'saveAs'] : [type === 'reply.chat' ? 'message' : 'seconds']).map((fieldKey) => ({ nodeId: 'step', fieldKey }))
  );
});

test.each(['reply.timeout', 'reply.ban', 'reply.mod', 'reply.vip', 'reply.points', 'reply.points_set'])('%s requires the ID of a set viewer', (type) => {
  const flow = newFlow('Missing set viewer');
  const step = nodeOf(type, { who: 'user', login: 'bagel', seconds: 600, amount: 10, on: true });
  flow.nodes = [{ ...nodeOf('twitch.follow'), id: 'trigger' }, step];
  flow.edges = [{ id: 'edge', from: 'trigger', port: 'next', to: 'step' }];
  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  const issues = fieldIssues(locateIssues(flow, validateDefinition(lowered.value)), flow.nodes);
  expect({ issues, missing: missingField(step) }).toEqual({
    issues: [{ path: 'nodes.step.params.user_id', code: 'required', nodeId: 'step', fieldKey: 'login' }],
    missing: 'login'
  });
});

test.each<{ type: FlowNode['type']; key: string; config: FlowNode['config']; expected: boolean }>([
  { type: 'cond.compare', key: 'right', config: { op: 'contains' }, expected: true },
  { type: 'cond.compare', key: 'right', config: { op: 'eq' }, expected: false },
  { type: 'reply.prediction_end', key: 'winner', config: { status: 'resolve' }, expected: true },
  { type: 'reply.prediction_end', key: 'winner', config: { status: 'cancel' }, expected: false },
  { type: 'reply.lights', key: 'scene', config: { mode: 'scene' }, expected: true },
  { type: 'reply.lights', key: 'scene', config: { mode: 'color' }, expected: false }
])('$type requires $key for $config', ({ type, key, config, expected }) => {
  expect(fieldRequired(nodeOf(type, config), key)).toBe(expected);
});

test.each([
  { code: 'user_not_resolved', type: 'cond.user', key: 'login' },
  { code: 'wait_too_long', type: 'reply.wait', key: 'seconds' },
  { code: 'delay_budget', type: 'reply.wait', key: 'seconds' },
  { code: 'constructor', type: 'reply.wait', key: undefined },
  { code: 'new_code', type: 'reply.wait', key: undefined }
])('maps node issue $code to $key', ({ code, type, key }) => {
  expect(fieldIssues([{ path: 'nodes.step', code }], [nodeOf(type)])[0].fieldKey).toBe(key);
});

test.each<{ type: FlowNode['type']; config: FlowNode['config']; missing: string | undefined }>([
  { type: 'twitch.follow', config: {}, missing: undefined },
  { type: 'twitch.chat.command', config: {}, missing: 'trigger' },
  { type: 'twitch.reward', config: { reward: 'setup-required' }, missing: 'reward' },
  { type: 'cond.cooldown', config: { seconds: 30 }, missing: undefined },
  { type: 'cond.compare', config: { op: 'gte' }, missing: 'left' },
  { type: 'reply.chat', config: { message: ' ' }, missing: 'message' },
  { type: 'reply.chat', config: { message: 'Thanks {user}!' }, missing: undefined },
  { type: 'reply.shoutout', config: { target: '{user}' }, missing: undefined },
  { type: 'reply.lights', config: { mode: 'color' }, missing: 'device' },
  { type: 'reply.lights', config: { device: 'desk', mode: 'scene' }, missing: 'scene' }
])('$type with $config is missing $missing', ({ type, config, missing }) => {
  expect(missingField(nodeOf(type, config))).toBe(missing);
});
