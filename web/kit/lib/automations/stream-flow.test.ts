// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { compileFlow, definitionToFlow, flowToDefinition, locateIssues, type Flow, type FlowConfig, type FlowResult } from './flow-graph';
import { exportFlows, SETUP_LOGIN } from './flow-share';
import type { Action, Definition } from './types';
import { validateDefinition } from './validate';

const manual = (actions: Action[]): Definition => ({ version: 1, trigger: { type: 'manual', params: {} }, actions });
const meta = { id: 'f', name: 'Stream steps', enabled: true };
const cheer = { type: 'event', params: { kind: 'channel.cheer' } };
const streamDefinition = (actions: Action[]): Definition => ({ version: 1, trigger: cheer, actions });

function valueOf<T>(result: FlowResult<T>): T {
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

test('stream steps keep event templates and separate options across a save', () => {
  const flow: Flow = {
    id: 'f', name: 'f', enabled: true,
    nodes: [
      { id: 't', type: 'twitch.cheer', x: 0, y: 0, config: { minBits: 1 } },
      { id: 'p', type: 'reply.poll', x: 0, y: 0, config: { title: '{bits} bits', choices: 'Yes {user}\nNo', seconds: 60, extraVotes: false } }
    ],
    edges: [{ id: 'e', from: 't', port: 'next', to: 'p' }]
  };
  const lowered = flowToDefinition(flow);
  if (!lowered.ok) throw new Error(JSON.stringify(lowered.issues));
  expect(validateDefinition(lowered.value)).toEqual([]);
  expect(lowered.value.actions?.[0]?.params).toEqual({ title: '{event.amount} bits', choices: ['Yes {user}', 'No'], seconds: 60 });
  const reopened = definitionToFlow(lowered.value, flow);
  if (!reopened.ok) throw new Error(JSON.stringify(reopened.issues));
  expect(reopened.value.nodes.find((node) => node.type === 'reply.poll')?.config).toEqual(flow.nodes[1]!.config);
});

function unwrap<T>(result: FlowResult<T>): T {
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

const follow = (actions: Action[]): Definition => ({ version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, actions });
const meta = { id: 'f', name: 'f', enabled: true };
const poll = { title: 'Pick', choices: ['a', 'b'], seconds: 60 };
const pollConfig = { title: 'Pick', choices: 'a\nb', seconds: 60 };

const roundtrips: { name: string; action: Action; config: FlowConfig; node?: string; saved?: Action['params'] }[] = [
  { name: 'chat', action: { type: 'chat', params: { text: 'Hi' } }, config: { message: 'Hi', reply: 'off' }, node: 'reply.chat' },
  { name: 'mention', action: { type: 'reply', params: { text: 'Hi' } }, config: { message: 'Hi', reply: 'mention' }, node: 'reply.chat' },
  { name: 'thread', action: { type: 'reply', params: { text: 'Hi', thread: true } }, config: { message: 'Hi', reply: 'thread' }, node: 'reply.chat' },
  { name: 'announcement with blank color', action: { type: 'announce', params: { text: 'Hi', color: '' } }, config: { text: 'Hi', color: 'primary' }, saved: { text: 'Hi', color: 'primary' } },
  { name: 'announcement with absent color', action: { type: 'announce', params: { text: 'Hi' } }, config: { text: 'Hi', color: 'primary' }, saved: { text: 'Hi', color: 'primary' } },
  { name: 'pin', action: { type: 'pin', params: { text: 'Welcome {user}' } }, config: { text: 'Welcome {user}' } },
  { name: 'delete', action: { type: 'delete_message', params: {} }, config: {} },
  { name: 'slow mode', action: { type: 'chat_mode', params: { mode: 'slow', on: true, value: 45 } }, config: { mode: 'slow', on: true, wait: 45 } },
  { name: 'legacy slow mode', action: { type: 'chat_mode', params: { mode: 'slow', on: true, value: 0 } }, config: { mode: 'slow', on: true, wait: 30 }, saved: { mode: 'slow', on: true, value: 30 } },
  { name: 'followers only', action: { type: 'chat_mode', params: { mode: 'followers', on: true, value: 129600 } }, config: { mode: 'followers', on: true, followAge: 129600 } },
  { name: 'chat delay', action: { type: 'chat_mode', params: { mode: 'chat_delay', on: true, value: 4 } }, config: { mode: 'chat_delay', on: true, delay: '4' } },
  { name: 'slow mode off', action: { type: 'chat_mode', params: { mode: 'slow', on: false, value: 45 } }, config: { mode: 'slow', on: false }, saved: { mode: 'slow', on: false } },
  { name: 'poll without extra votes', action: { type: 'poll', params: poll }, config: { ...pollConfig, extraVotes: false } },
  { name: 'poll with zero points per vote', action: { type: 'poll', params: { ...poll, points_per_vote: 0 } }, config: { ...pollConfig, extraVotes: false }, saved: poll },
  { name: 'poll with extra votes', action: { type: 'poll', params: { ...poll, points_per_vote: 100 } }, config: { ...pollConfig, extraVotes: true, pointsPerVote: 100 } },
  { name: 'prediction winner by name', action: { type: 'prediction_end', params: { status: 'resolve', outcome: 'Blue' } }, config: { status: 'resolve', outcome: 'Blue' } },
  { name: 'legacy prediction winner', action: { type: 'prediction_end', params: { status: 'resolve', winner: 2 } }, config: { status: 'resolve', outcome: '2' }, saved: { status: 'resolve', outcome: '2' } }
];

test.each(roundtrips)('$name opens and lowers', (row) => {
  const flow = unwrap(definitionToFlow(follow([row.action]), meta));
  const step = flow.nodes[1];
  const lowered = unwrap(flowToDefinition(flow));
  expect({ type: step?.type, config: step?.config, action: lowered.actions?.[0] }).toEqual({
    type: row.node ?? `reply.${row.action.type}`, config: row.config, action: { ...row.action, params: row.saved ?? row.action.params }
  });
});

test.each([
  { type: 'reply.reply', action: { type: 'reply', params: { text: 'Hi' } }, config: { text: 'Hi' } },
  { type: 'reply.poll', action: { type: 'poll', params: poll }, config: { ...pollConfig, choices: 'a\nb\n' } },
  { type: 'reply.poll', action: { type: 'poll', params: poll }, config: { ...pollConfig, choices: 'a\n  \nb', extraVotes: false, pointsPerVote: 100 } },
  { type: 'reply.prediction_end', action: { type: 'prediction_end', params: { status: 'lock' } }, config: { status: 'lock', outcome: '{not:a-template}' } }
] satisfies { type: string; action: Action; config: FlowConfig }[])('$type lowers a legacy or hidden field config', ({ type, action, config }) => {
  const flow = unwrap(definitionToFlow(follow([action]), meta));
  Object.assign(flow.nodes[1]!, { type, config });
  expect(unwrap(flowToDefinition(flow)).actions?.[0]).toEqual(action);
});

test('delete message reports a trigger mismatch on the node after a follow', () => {
  const definition = follow([{ type: 'delete_message', params: {} }]);
  const flow = unwrap(definitionToFlow(definition, meta));
  const node = flow.nodes[1]!;
  expect(locateIssues(flow, validateDefinition(definition))).toContainEqual({ path: `nodes.${node.id}.type`, code: 'trigger_mismatch' });
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

test.each(['raid', 'mod', 'vip', 'whisper', 'unban'])('sharing %s removes a literal account and disables the action', (type) => {
  const extra = type === 'whisper' ? { text: 'Hello' } : { on: true };
  const params = type === 'raid' || type === 'unban' ? { login: 'specificviewer' } : { login: 'specificviewer', ...extra };
  const exported = exportFlows([{ id: 'one', name: 'one', definition: manual([{ type, params }]) }]);
  const serialized = JSON.stringify(exported.file);
  expect(serialized).not.toContain('specificviewer');
  expect(serialized).toContain(SETUP_LOGIN);
  const action = exported.file.flows[0]!.definition.actions?.[0];
  expect(action?.enabled).toBe(false);
});

test('sharing a stream action preserves a dynamic viewer target', () => {
  const definition = manual([{ type: 'vip', params: { login: '{user}', on: true } }]);
  const exported = exportFlows([{ id: 'one', name: 'one', definition }]);
  expect(exported.file.flows[0]!.definition.actions?.[0]).toEqual(definition.actions?.[0]);
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

const setTitle = { type: 'set_title', params: { title: 'New title' } };
const setGame = { type: 'set_game', params: { game: 'Chess' } };
const streamInfo = { title: '', game: '', tags: '', branded: 'keep' };

test.each([
  { name: 'consecutive fields', actions: [setTitle, { ...setGame, enabled: true }], configs: [{ ...streamInfo, title: 'New title', game: 'Chess' }] },
  { name: 'repeated field', actions: [setTitle, setTitle], configs: [streamInfo, streamInfo].map((config) => ({ ...config, title: 'New title' })) },
  { name: 'different enabled flags', actions: [setTitle, { ...setGame, enabled: false }], configs: [{ ...streamInfo, title: 'New title' }, { ...streamInfo, game: 'Chess', disabled: true }] },
  { name: 'lone branded content', actions: [{ type: 'branded_content', params: { on: true } }], configs: [{ ...streamInfo, branded: 'on' }] }
] satisfies { name: string; actions: Action[]; configs: FlowConfig[] }[])('$name open as stream info', ({ actions, configs }) => {
  const opened = valueOf(definitionToFlow(streamDefinition(actions), meta));
  expect(opened.nodes.slice(1).map(({ type, config }) => ({ type, config })))
    .toEqual(configs.map((config) => ({ type: 'reply.stream_info', config })));
});

test.each([true, false])('stream info writes ordered, owned actions with enabled=%j', (enabled) => {
  const branded = { type: 'branded_content', params: { on: false } };
  const tags = { type: 'set_tags', params: { tags: 'Échecs, , 遊戲2' } };
  const title = { type: 'set_title', params: { title: '{event.amount} bits' } };
  const actions = [branded, tags, setGame, title].map((action) => ({ ...action, enabled }));
  const opened = valueOf(definitionToFlow(streamDefinition(actions), meta));
  const node = opened.nodes[1]!;
  const saved = valueOf(compileFlow(opened));
  const expected = [title, setGame, tags, branded].map((action) => enabled ? action : { ...action, enabled: false });
  const reopened = valueOf(definitionToFlow(saved.definition, meta));
  expect({
    actions: saved.definition.actions, issues: validateDefinition(saved.definition),
    owners: [...saved.nodeByPath].filter(([path]) => path.startsWith('actions')).map(([, owner]) => owner),
    config: reopened.nodes[1]?.config
  }).toEqual({
    actions: expected, issues: [], owners: expected.map(() => node.id),
    config: { title: '{bits} bits', game: 'Chess', tags: 'Échecs, , 遊戲2', branded: 'off', ...(enabled ? {} : { disabled: true }) }
  });
});

test.each([
  { action: { type: 'commercial', params: { seconds: 90 } }, config: { seconds: '90', confirm: true }, quiet: false },
  { action: { type: 'clip', params: { title: 'Highlight', seconds: 30, reply: '{clip:title} {clip:url}', quiet: true } }, config: { title: 'Highlight', seconds: 30, reply: '{clip:title} {clip:url}', confirm: false }, quiet: true }
] satisfies { action: Action; config: FlowConfig; quiet: boolean }[])('$action.type round trips with quiet=$quiet', ({ action, config, quiet }) => {
  const opened = valueOf(definitionToFlow(streamDefinition([action]), meta));
  const saved = valueOf(flowToDefinition(opened));
  expect({ config: opened.nodes[1]?.config, actions: saved.actions, issues: validateDefinition(saved) })
    .toEqual({ config, actions: [{ ...action, params: { ...action.params, quiet } }], issues: [] });
});
