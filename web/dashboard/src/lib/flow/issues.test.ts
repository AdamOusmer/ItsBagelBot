// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, locateIssues, validateDefinition } from '@bagel/kit/automations';
import { fieldIssues, fieldRequired, inlineFieldIssue, missingField } from './issues';
import { newFlow, specFor, type FlowNode } from './model';

const nodeOf = (type: FlowNode['type'], config: FlowNode['config'] = {}): FlowNode => ({ id: 'step', type, config, x: 0, y: 0 });

const mappings = [
  ['reply.chat', 'params.text', 'message'], ['reply.wait', 'params.ms', 'seconds'],
  ['reply.math', 'params.save_as', 'saveAs'], ['reply.http_get', 'params.json_path[0]', 'jsonPath'],
  ['reply.http_post', 'params.save_as', 'saveAs'], ['reply.counter', 'params.name', 'counter'],
  ['cond.cooldown', 'params.global_s', 'channelSeconds'], ['cond.cooldown', 'params.user_s', 'viewerSeconds'],
  ['cond.cooldown', 'params.left_command', 'leftCommand'], ['cond.cooldown', 'params.left_reply', 'leftReply'],
  ['reply.countdown_start', 'params.ready_reply', 'readyReply'],
  ['cond.user', 'params.allow_ids[0]', 'users'], ['cond.user', 'params.allow[0]', 'users'], ['cond.user', 'userId', 'users'],
  ['cond.user', 'params.deny_ids[0]', 'users'], ['cond.category', 'params.allow[1]', 'games'], ['cond.category', 'params.deny', 'games'],
  ['cond.follow', 'params.min_days', 'minDays'], ['cond.accountAge', 'params.min_days', 'minDays'],
  ['twitch.chat.command', 'params.name', 'trigger'], ['twitch.chat.word', 'params.name', 'word'],
  ['twitch.chat.command', 'params.aliases[0]', 'aliases'], ['twitch.chat.word', 'params.sources[0]', 'subMessages'],
  ['twitch.sub', 'params.filters.is_gift', 'variant'], ['twitch.cheer', 'params.filters.amount', 'minBits'],
  ['twitch.sub', 'params.filters.months', 'minMonths'], ['twitch.sub', 'params.filters.streak', 'minStreak'],
  ['twitch.sub_gift', 'params.filters.amount', 'minGifts'], ['twitch.sub_gift', 'params.filters.tier', 'tier'],
  ['twitch.sub_gift', 'params.filters.anonymous', 'gifter'], ['twitch.cheer', 'params.filters.anonymous', 'cheerer'],
  ['twitch.raid', 'params.filters.viewers', 'minViewers'], ['twitch.reward', 'params.reward_id', 'reward'],
  ['twitch.reward', 'params.title', 'reward'], ['twitch.reward', 'params.cost', 'reward'],
  ['twitch.reward', 'params.color', 'reward'], ['twitch.reward', 'params.cooldown_s', 'reward'],
  ['twitch.reward', 'params.on_redeem', 'reward'], ['twitch.reward', 'params.input_required', 'reward'],
  ['twitch.ad_break', 'params.filters.automatic', 'startedBy'], ['twitch.ad_break', 'params.filters.seconds', 'minLength'],
  ['twitch.channel_update', 'params.filters.category', 'category'],
  ['schedule.timer', 'params.interval_s', 'every'], ['schedule.timer', 'params.random_jitter_s', 'jitter'],
  ['schedule.timer', 'params.min_chat_lines', 'chatLines'], ['schedule.timer', 'params.offline_ok', 'offline'],
  ['cond.cooldown', 'params', 'channelSeconds'], ['cond.user', 'params', 'users'], ['cond.category', 'params', 'games'],
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
  ['reply.unban', 'params.login', 'target'],
  ['reply.poll', 'params.choices[1]', 'choices'], ['reply.prediction', 'params.outcomes[0]', 'outcomes'],
  ['reply.chat', 'message.bits', 'message'], ['reply.math', 'config.expression', 'expression'],
  ['reply.text', 'params.arg1', 'arg1'], ['reply.text', 'params.arg2', 'arg2'],
  ['reply.var', 'params.name', 'name'], ['reply.var', 'params.amount', 'amount'],
  ['reply.var', 'params.scope', 'scope'], ['reply.var', 'params.value', 'value'],
  ['cond.counter', 'params.name', 'counter'], ['reply.redemption', 'params.status', 'result'],
  ['reply.countdown_add', 'config.per', 'per']
];

test.each(mappings)('%s maps %s to %s', (type, path, fieldKey) => {
  expect(fieldIssues([{ path: `nodes.step.${path}`, code: 'required' }], [nodeOf(type)]))
    .toEqual([{ path: `nodes.step.${path}`, code: 'required', nodeId: 'step', fieldKey }]);
});

test.each([
  ['set', false], ['add', true], ['subtract', true], ['forget', false]
])('variable %s uses the requirements of its saved action', (change, amount) => {
  const node = nodeOf('reply.var', { do: change, scope: 'channel' });
  expect({ missing: missingField(node), amount: fieldRequired(node, 'amount') }).toEqual({ missing: 'name', amount });
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
  ['cond.user', 'users', true], ['cond.category', 'games', true], ['cond.pattern', 'pattern', true],
  ['cond.cooldown', 'channelSeconds', true], ['cond.cooldown', 'name', false],
  ['twitch.chat.command', 'trigger', true], ['twitch.reward', 'reward', true], ['reply.shoutout', 'target', true],
  ['reply.raid', 'target', true], ['reply.mod', 'target', false], ['reply.vip', 'target', false],
  ['reply.whisper', 'target', true], ['reply.timeout', 'target', false],
  ['reply.countdown_start', 'name', true], ['reply.countdown_start', 'seconds', true], ['reply.countdown_start', 'leftReply', false],
  ['reply.points', 'amount', true]
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

test.each(['reply.timeout', 'reply.ban', 'reply.unban', 'reply.mod', 'reply.vip', 'reply.points', 'reply.points_set'])('%s requires the ID of a set viewer', (type) => {
  const flow = newFlow('Missing set viewer');
  const step = nodeOf(type, { who: 'user', login: 'bagel', seconds: 600, amount: 10, change: 'add' });
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
  { type: 'cond.compare', key: 'right', config: { op: 'starts' }, expected: true },
  { type: 'cond.compare', key: 'right', config: { op: 'eq' }, expected: false },
  { type: 'cond.var', key: 'value', config: { op: 'contains' }, expected: true },
  { type: 'cond.var', key: 'value', config: { op: 'starts' }, expected: true },
  { type: 'cond.var', key: 'value', config: { op: 'not_exists' }, expected: false },
  { type: 'reply.prediction_end', key: 'outcome', config: { status: 'resolve' }, expected: true },
  { type: 'reply.prediction_end', key: 'outcome', config: { status: 'cancel' }, expected: false },
  { type: 'reply.lights', key: 'scene', config: { mode: 'scene' }, expected: true },
  { type: 'reply.lights', key: 'scene', config: { mode: 'color' }, expected: false },
  { type: 'reply.counter', key: 'amount', config: { change: 'set' }, expected: false },
  { type: 'reply.counter', key: 'value', config: { change: 'set' }, expected: true }
])('$type requires $key for $config', ({ type, key, config, expected }) => {
  expect(fieldRequired(nodeOf(type, config), key)).toBe(expected);
});

test.each([
  { percent: 50.5, fields: [{ fieldKey: 'percent', code: 'invalid_format' }] },
  { percent: 50, fields: [] }
])('chance at $percent reports $fields', ({ percent, fields }) => {
  const flow = newFlow('Chance');
  flow.nodes = [{ ...nodeOf('twitch.follow'), id: 'trigger' }, nodeOf('cond.chance', { percent })];
  flow.edges = [{ id: 'edge', from: 'trigger', port: 'next', to: 'step' }];
  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  const issues = fieldIssues(locateIssues(flow, validateDefinition(lowered.value)), flow.nodes);
  expect(issues.map(({ fieldKey, code }) => ({ fieldKey, code }))).toEqual(fields);
});

test.each([
  { node: nodeOf('reply.countdown_start', { name: 'tea_time' }), key: 'name', code: 'simple_word' },
  { node: nodeOf('reply.countdown_start', { name: 'clock2' }), key: 'name', code: undefined },
  { node: nodeOf('reply.points', { change: 'add' }), key: 'amount', code: 'required' },
  { node: nodeOf('reply.points', { change: 'take', amount: 0 }), key: 'amount', code: 'out_of_range' },
  { node: nodeOf('reply.points', { amount: -10 }), key: 'amount', code: undefined },
  { node: nodeOf('reply.counter', { change: 'add', amount: 0 }), key: 'amount', code: 'out_of_range' },
  { node: nodeOf('reply.countdown_add', { per: 0 }), key: 'per', code: 'out_of_range' },
  { node: nodeOf('reply.reward_update', { setEnabled: 'keep', paused: 'keep', cooldown: '', reset: true }), key: 'setEnabled', code: 'required' },
  { node: nodeOf('reply.reward_update', { setEnabled: 'keep', paused: 'resume' }), key: 'setEnabled', code: undefined },
  { node: nodeOf('reply.reward_update', { setEnabled: 'keep', paused: 'keep', cooldown: 0 }), key: 'setEnabled', code: undefined }
])('reports $code on $node.type $key while editing', ({ node, key, code }) => {
  const field = specFor(node.type)!.fields.find((field) => field.key === key)!;
  expect(inlineFieldIssue(node, field)?.code).toBe(code);
});

test.each([
  { code: 'user_not_resolved', type: 'cond.user', key: 'users' },
  { code: 'unknown_port', type: 'cond.cooldown', key: undefined },
  { code: 'wait_too_long', type: 'reply.wait', key: 'seconds' },
  { code: 'delay_budget', type: 'reply.wait', key: 'seconds' },
  { code: 'constructor', type: 'reply.wait', key: undefined },
  { code: 'new_code', type: 'reply.wait', key: undefined }
])('maps node issue $code to $key', ({ code, type, key }) => {
  expect(fieldIssues([{ path: 'nodes.step', code }], [nodeOf(type)])[0].fieldKey).toBe(key);
});

test.each<{ type: FlowNode['type']; config: FlowNode['config']; mismatch: boolean }>([
  { type: 'twitch.follow', config: {}, mismatch: true },
  { type: 'twitch.chat.command', config: { trigger: '!check' }, mismatch: false },
  { type: 'twitch.chat.word', config: { word: 'check' }, mismatch: false }
])('the command text check accepts chat triggers: $type', ({ type, config, mismatch }) => {
  const flow = newFlow('Check trigger');
  flow.nodes = [{ ...nodeOf(type, config), id: 'start' }, nodeOf('cond.pattern', { pattern: '^[0-9]+$' }), { ...nodeOf('reply.chat', { message: 'OK' }), id: 'reply' }];
  flow.edges = [{ id: 'check', from: 'start', port: 'next', to: 'step' }, { id: 'yes', from: 'step', port: 'yes', to: 'reply' }];
  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  expect(locateIssues(flow, validateDefinition(lowered.value))).toEqual(mismatch ? [{ path: 'nodes.step.type', code: 'trigger_mismatch' }] : []);
});

test.each<{ type: FlowNode['type']; config: FlowNode['config']; missing: string | undefined }>([
  { type: 'twitch.follow', config: {}, missing: undefined },
  { type: 'twitch.chat.command', config: {}, missing: 'trigger' },
  { type: 'twitch.reward', config: { reward: 'setup-required' }, missing: 'reward' },
  { type: 'cond.user', config: {}, missing: 'users' },
  { type: 'cond.user', config: { userId: '42', login: 'lotti' }, missing: undefined },
  { type: 'cond.cooldown', config: { channelSeconds: 30, viewerSeconds: 0 }, missing: undefined },
  { type: 'cond.cooldown', config: { channelSeconds: 0, viewerSeconds: 30 }, missing: undefined },
  { type: 'cond.cooldown', config: { channelSeconds: 0, viewerSeconds: 0 }, missing: 'channelSeconds' },
  { type: 'cond.compare', config: { op: 'gte' }, missing: 'left' },
  { type: 'reply.chat', config: { message: ' ' }, missing: 'message' },
  { type: 'reply.chat', config: { message: 'Thanks {user}!' }, missing: undefined },
  { type: 'reply.shoutout', config: { target: '{user}' }, missing: undefined },
  { type: 'reply.lights', config: { mode: 'color' }, missing: 'device' },
  { type: 'reply.lights', config: { device: 'desk', mode: 'scene' }, missing: 'scene' }
])('$type with $config is missing $missing', ({ type, config, missing }) => {
  expect(missingField(nodeOf(type, config))).toBe(missing);
});
