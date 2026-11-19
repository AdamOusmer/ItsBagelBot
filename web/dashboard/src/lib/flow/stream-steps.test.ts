// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { definitionToFlow, flowToDefinition, validateDefinition, type FlowResult } from '@bagel/kit/automations';
import { describe } from './describe';
import { addStep } from './editor';
import { english } from './english.testing';
import { newFlow, specFor, type FlowNode } from './model';
import { addNode } from './ops.testing';
import { rehearseFlow, type RehearsalStep } from './rehearse';
import type { RehearsalMode } from '@bagel/kit/engine/rehearsal';
import { searchSteps } from './search';

type StepCase = { type: string; config: FlowNode['config']; line: string; effect?: string; kind: RehearsalStep['kind']; mode?: RehearsalMode; color?: string; message?: string };
const steps: StepCase[] = [
  { type: 'reply.shoutout', config: { target: '{user}' }, line: 'Shoutout to {user}', effect: 'Shoutout to PrincessBarney', kind: 'effect' },
  { type: 'reply.announce', config: { text: 'Welcome {user} and {viewers} viewers!', color: 'orange' }, line: 'Announce: Welcome {user} and {viewers} viewers!', kind: 'say', mode: 'announce', color: 'orange', message: 'Welcome PrincessBarney and 42 viewers!' },
  { type: 'reply.chat', config: { message: 'Welcome {viewers} viewers!', reply: 'mention' }, line: 'Welcome {viewers} viewers!', kind: 'say', mode: 'chat', message: '@PrincessBarney Welcome 42 viewers!' },
  { type: 'reply.timeout', config: { target: '{user}', seconds: 60, reason: 'Please stop, {viewers} viewers are watching' }, line: 'Timeout {user} for 1 minute', effect: 'Timeout PrincessBarney for 1 minute', kind: 'effect' }
];

function valueOf<T>(result: FlowResult<T>): T {
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

test.each(steps)('$type saves, reopens, describes and rehearses its fields', ({ type, config, line, effect, kind, mode, color, message }) => {
  const flow = newFlow('Stream steps');
  const root = addNode(flow, 'twitch.raid', { x: 0, y: 0 });
  const node = addNode(flow, type, { x: 0, y: 0 }, { from: root.id, port: 'next' });
  node.config = config;
  const saved = valueOf(flowToDefinition(flow));
  const opened = valueOf(definitionToFlow(saved, flow));
  expect({ issues: validateDefinition(saved), config: opened.nodes.find((step) => step.type === type)!.config }).toEqual({ issues: [], config });
  expect(describe(english, node)).toBe(line);
  const rehearsal = rehearseFlow(opened, english);
  const rehearsed = rehearsal.steps.at(-1)!;
  const body = rehearsed.chatLines.at(0);
  expect({ starts: rehearsal.starts, kind: rehearsed.kind, mode: body?.mode, color: body?.color, message: rehearsed.lines?.[0], text: rehearsed.text })
    .toEqual({ starts: true, kind, mode, color, message, text: effect ?? '' });
  expect(body?.segments.map((segment) => segment.text).join('')).toBe(message);
});

test.each(steps)('$type validates immediately after adding', ({ type }) => {
  const flow = newFlow('Defaults');
  const root = addNode(flow, 'twitch.raid', { x: 0, y: 0 });
  addStep(flow, {
    type, at: { x: 0, y: 0 }, placement: { kind: 'port', from: root.id, port: 'next', mode: 'parallel' },
    chatMessage: english('flows.board.defaultChatMessage')
  });
  expect(validateDefinition(valueOf(flowToDefinition(flow)))).toEqual([]);
});

test.each([
  ['shoutout', 'reply.shoutout'], ['so', 'reply.shoutout'], ['announce', 'reply.announce'],
  ['timeout', 'reply.timeout'], ['reply', 'reply.chat'], ['stream ends', 'twitch.stream.offline'], ['offline', 'twitch.stream.offline']
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
  { type: 'twitch.chat.command', gated: false, codes: ['perm_required'] },
  { type: 'twitch.chat.command', gated: true, codes: [] },
  { type: 'twitch.chat.word', gated: false, codes: ['perm_required'] },
  { type: 'twitch.chat.word', gated: true, codes: [] }
])('timeout on $type needs a leading role check: $gated', ({ type, gated, codes }) => {
  const flow = newFlow('Permission');
  const root = addNode(flow, type, { x: 0, y: 0 });
  root.config = type === 'twitch.chat.command' ? { trigger: '!timeout' } : { word: 'timeout' };
  const gate = addNode(flow, 'cond.role', { x: 0, y: 0 }, { from: root.id, port: 'next' });
  gate.config.level = gated ? 'mod' : 'everyone';
  addNode(flow, 'reply.timeout', { x: 0, y: 0 }, { from: gate.id, port: 'yes' });
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
