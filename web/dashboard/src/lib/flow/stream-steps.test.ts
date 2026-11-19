// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { definitionToFlow, flowToDefinition, validateDefinition, type FlowResult, type Params } from '@bagel/kit/automations';
import { describe } from './describe';
import { english } from './english.testing';
import { newFlow, specFor, targetMode, type FlowNode } from './model';
import { addNode } from './ops.testing';
import { rehearseFlow, type RehearsalScenario, type RehearsalStep } from './rehearse';
import type { RehearsalMode } from '@bagel/kit/engine/rehearsal';
import { searchSteps } from './search';

type StepCase = { type: string; config: FlowNode['config']; line: string; effect?: string; kind: RehearsalStep['kind']; status?: RehearsalStep['status']; mode?: RehearsalMode; color?: string; message?: string; trigger?: string; scenario?: Partial<RehearsalScenario> };
const steps: StepCase[] = [
  { type: 'reply.shoutout', config: { target: '{user}' }, line: 'Shoutout to {user}', effect: 'Shoutout to PrincessBarney', kind: 'effect' },
  { type: 'reply.announce', config: { text: 'Welcome {user} and {viewers} viewers!', color: 'orange' }, line: 'Announce: Welcome {user} and {viewers} viewers!', kind: 'say', mode: 'announce', color: 'orange', message: 'Welcome PrincessBarney and 42 viewers!' },
  { type: 'reply.reply', config: { text: 'Welcome {viewers} viewers!' }, line: 'Reply to viewer: Welcome {viewers} viewers!', kind: 'say', mode: 'chat', message: '@PrincessBarney Welcome 42 viewers!' },
  { type: 'reply.timeout', config: { target: '{user}', seconds: 60, reason: 'Please stop, {viewers} viewers are watching' }, line: 'Timeout {user} for 1m', effect: 'Timeout PrincessBarney for 1m', kind: 'effect' },
  { type: 'reply.ban', config: { who: 'viewer', reason: 'Repeated spam' }, line: 'Ban the viewer', effect: 'Ban PrincessBarney', kind: 'effect' },
  { type: 'reply.unban', trigger: 'twitch.chat.command', scenario: { role: 'mod' }, config: { who: 'mention' }, line: 'Lift timeout or ban for the @mentioned viewer', effect: 'Lift timeout or ban for Lotti', kind: 'effect' },
  { type: 'reply.unban', trigger: 'twitch.chat.command', scenario: { role: 'mod', message: '!unban someone' }, config: { who: 'mention' }, line: 'Lift timeout or ban for the @mentioned viewer', effect: 'No viewer was @mentioned', kind: 'effect', status: 'failed' },
  { type: 'reply.ban', config: { who: 'login', target: '{var:absent}' }, line: 'Ban {var:absent}', effect: 'The name came out empty', kind: 'effect', status: 'failed' },
  { type: 'reply.mod', config: { change: 'add', who: 'user', userId: '123', login: 'Lotti' }, line: 'Make Lotti a moderator', effect: 'Make Lotti a moderator', kind: 'effect' },
  { type: 'reply.vip', config: { change: 'remove', who: 'viewer' }, line: 'Remove the viewer as VIP', effect: 'Remove PrincessBarney as VIP', kind: 'effect' },
  { type: 'reply.unban_resolve', trigger: 'twitch.unban_request', config: { status: 'approve', resolution: 'Thanks for writing' }, line: 'Approve the unban request', effect: 'Approve the unban request', kind: 'effect' },
  { type: 'reply.unban_resolve', trigger: 'twitch.unban_request', config: { status: 'deny', resolution: '' }, line: 'Deny the unban request', effect: 'Deny the unban request', kind: 'effect' }
];

function valueOf<T>(result: FlowResult<T>): T {
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

function stepAfter(flow: ReturnType<typeof newFlow>, trigger: string): { from: string; port: string } {
  const at = { x: 0, y: 0 };
  const root = addNode(flow, trigger, at);
  if (trigger !== 'twitch.chat.command') return { from: root.id, port: 'next' };
  root.config.trigger = '!unban';
  return { from: addNode(flow, 'cond.role', at, { from: root.id, port: 'next' }).id, port: 'yes' };
}

test.each(steps)('$type saves, reopens, describes and rehearses its fields', ({ type, config, line, effect = '', kind, status = 'ok', mode, color, message, trigger = 'twitch.raid', scenario }) => {
  const flow = newFlow('Stream steps');
  const node = addNode(flow, type, { x: 0, y: 0 }, stepAfter(flow, trigger));
  node.config = config;
  const saved = valueOf(flowToDefinition(flow));
  const opened = valueOf(definitionToFlow(saved, flow));
  expect({ issues: validateDefinition(saved), config: opened.nodes.find((step) => step.type === type)!.config, line: describe(english, node) })
    .toEqual({ issues: [], config, line });
  const rehearsal = rehearseFlow(opened, english, { scenario });
  const rehearsed = rehearsal.steps.at(-1)!;
  const body = rehearsed.chatLines.at(0);
  expect({ starts: rehearsal.starts, kind: rehearsed.kind, status: rehearsed.status, mode: body?.mode, color: body?.color, message: rehearsed.lines?.[0], text: rehearsed.text })
    .toEqual({ starts: true, kind, status, mode, color, message, text: effect });
  expect(body?.segments.map((segment) => segment.text).join('')).toBe(message);
});

test.each(steps)('$type validates immediately after adding', ({ type, trigger }) => {
  const flow = newFlow('Defaults');
  const root = addNode(flow, trigger === 'twitch.unban_request' ? trigger : 'twitch.raid', { x: 0, y: 0 });
  addNode(flow, type, { x: 0, y: 0 }, { from: root.id, port: 'next' });
  expect(validateDefinition(valueOf(flowToDefinition(flow)))).toEqual([]);
});

test.each<{ params: Params; config: FlowNode['config'] }>([
  { params: { login: 'bob', on: true }, config: { target: 'bob', change: 'add' } },
  { params: { login: 'bob' }, config: { target: 'bob' } }
])('legacy moderator action preserves the login mode and saved params: $params', ({ params, config }) => {
  const definition = {
    version: 1,
    trigger: { type: 'event', params: { kind: 'channel.raid' } },
    actions: [{ type: 'mod', params }]
  };
  const opened = valueOf(definitionToFlow(definition, newFlow('Legacy moderator role')));
  const node = opened.nodes.find((step) => step.type === 'reply.mod')!;
  expect({ config: node.config, who: targetMode(node.config), saved: valueOf(flowToDefinition(opened)) })
    .toEqual({ config, who: 'login', saved: definition });
});

test.each([
  ['shoutout', 'reply.shoutout'], ['so', 'reply.shoutout'], ['announce', 'reply.announce'],
  ['timeout', 'reply.timeout'], ['reply', 'reply.reply'], ['stream ends', 'twitch.stream.offline'], ['offline', 'twitch.stream.offline'],
  ['ban', 'reply.ban'], ['lift timeout', 'reply.unban'], ['moderator', 'reply.mod'],
  ['unban request', 'twitch.unban_request']
])('%s finds %s first', (query, type) => {
  expect(searchSteps(english, query)[0]?.type).toBe(type);
});

test.each<{ type: string; config: FlowNode['config']; code: string }>([
  { type: 'reply.shoutout', config: { target: '' }, code: 'required' },
  { type: 'reply.announce', config: { text: 'Hi', color: 'red' }, code: 'invalid_enum' },
  { type: 'reply.timeout', config: { target: '{user}', seconds: 0 }, code: 'out_of_range' },
  { type: 'reply.timeout', config: { target: '{user}', seconds: 1209601 }, code: 'out_of_range' }
])('$type rejects invalid fields: $config', ({ type, config, code }) => {
  const flow = newFlow('Validation');
  const root = addNode(flow, 'twitch.raid', { x: 0, y: 0 });
  addNode(flow, type, { x: 0, y: 0 }, { from: root.id, port: 'next' }).config = config;
  expect(validateDefinition(valueOf(flowToDefinition(flow))).map((issue) => issue.code)).toContain(code);
});

test.each([1, 1209600])('timeout accepts boundary %s', (seconds) => {
  const spec = specFor('reply.timeout')!;
  expect(spec.fields.find((field) => field.key === 'seconds')).toMatchObject({ min: 1, max: 1209600 });
  const flow = newFlow('Timeout');
  const root = addNode(flow, 'twitch.raid', { x: 0, y: 0 });
  addNode(flow, spec.type, { x: 0, y: 0 }, { from: root.id, port: 'next' }).config.seconds = seconds;
  expect(validateDefinition(valueOf(flowToDefinition(flow)))).toEqual([]);
});

test.each([
  { type: 'twitch.chat.command', step: 'reply.timeout', gated: false, codes: ['perm_required'] },
  { type: 'twitch.chat.command', step: 'reply.timeout', gated: true, codes: [] },
  { type: 'twitch.chat.word', step: 'reply.timeout', gated: false, codes: [] },
  { type: 'twitch.chat.word', step: 'reply.timeout', gated: true, codes: [] },
  { type: 'twitch.chat.word', step: 'reply.mod', gated: false, codes: ['perm_required'] }
])('$step on $type with a leading role check: $gated', ({ type, step, gated, codes }) => {
  const flow = newFlow('Permission');
  const root = addNode(flow, type, { x: 0, y: 0 });
  root.config = type === 'twitch.chat.command' ? { trigger: '!timeout' } : { word: 'timeout' };
  const gate = addNode(flow, 'cond.role', { x: 0, y: 0 }, { from: root.id, port: 'next' });
  gate.config.level = gated ? 'mod' : 'everyone';
  addNode(flow, step, { x: 0, y: 0 }, { from: gate.id, port: 'yes' });
  expect(validateDefinition(valueOf(flowToDefinition(flow))).map((issue) => issue.code)).toEqual([...codes]);
});

test('offline trigger saves, reopens and rehearses', () => {
  const flow = newFlow('Stream ends');
  const root = addNode(flow, 'twitch.stream.offline', { x: 0, y: 0 });
  addNode(flow, 'reply.chat', { x: 0, y: 0 }, { from: root.id, port: 'next' }).config.message = 'See you soon!';
  const saved = valueOf(flowToDefinition(flow));
  expect({ trigger: saved.trigger, issues: validateDefinition(saved) }).toEqual({ trigger: { type: 'event', params: { kind: 'stream.offline' } }, issues: [] });
  const opened = valueOf(definitionToFlow(saved, flow));
  expect(opened.nodes[0]?.type).toBe(root.type);
  const rehearsal = rehearseFlow(opened, english);
  expect({ starts: rehearsal.starts, live: rehearsal.scenario.live, summary: rehearsal.summary, steps: rehearsal.steps.map(({ kind, text, lines }) => ({ kind, text, lines })) })
    .toEqual({ starts: true, live: false, summary: 'Your stream ends', steps: [
      { kind: 'trigger', text: 'Your stream ends', lines: undefined },
      { kind: 'say', text: '', lines: ['See you soon!'] }
    ] });
});

test.each([
  { user: 'Lotti', message: '@Lotti Thanks for coming!' },
  { user: '@Lotti', message: '@Lotti Thanks for coming!' }
])('viewer reply preserves the executor prefix for $user', ({ user, message }) => {
  const flow = newFlow('Viewer reply');
  const root = addNode(flow, 'twitch.raid', { x: 0, y: 0 });
  addNode(flow, 'reply.reply', { x: 0, y: 0 }, { from: root.id, port: 'next' }).config.text = 'Thanks for coming!';
  const step = rehearseFlow(flow, english, { scenario: { user } }).steps.at(-1)!;
  expect([step.lines, step.chatLines[0]?.segments]).toEqual([[message], [{ kind: 'plain', text: message }]]);
});
