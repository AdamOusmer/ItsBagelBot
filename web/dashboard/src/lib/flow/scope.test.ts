// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { newFlow, variablesFor, type Flow, type FlowNode } from './model';

type Step = [FlowNode['id'], FlowNode['type'], FlowNode['config']?];
type Link = [string, string, string];
type ScopeCase = { name: string; steps: Step[]; links: Link[]; target: string; tokens: string[] };
const viewer = ['{user}', '{user.id}'];
const base: Step = ['root', 'twitch.follow'];

const cases: ScopeCase[] = [
  { name: 'linear ancestors', steps: [base, ['a', 'reply.counter', { counter: 'wins' }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: [...viewer, '{count}'] },
  { name: 'parallel sibling', steps: [base, ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['root', 'next', 'b']], target: 'b', tokens: viewer },
  { name: 'parallel merge', steps: [base, ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.wait'], ['join', 'reply.chat']], links: [['root', 'next', 'a'], ['root', 'next', 'b'], ['a', 'next', 'join'], ['b', 'next', 'join']], target: 'join', tokens: viewer },
  { name: 'same name on every path', steps: [base, ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.math', { saveAs: 'points' }], ['join', 'reply.chat']], links: [['root', 'next', 'a'], ['root', 'next', 'b'], ['a', 'next', 'join'], ['b', 'next', 'join']], target: 'join', tokens: [...viewer, '{local:points}'] },
  { name: 'cooldown rejected port', steps: [base, ['gate', 'cond.cooldown'], ['b', 'reply.chat']], links: [['root', 'next', 'gate'], ['gate', 'no', 'b']], target: 'b', tokens: [...viewer, '{cooldown:left}'] },
  { name: 'cooldown ports merge', steps: [base, ['gate', 'cond.cooldown'], ['b', 'reply.chat']], links: [['root', 'next', 'gate'], ['gate', 'yes', 'b'], ['gate', 'no', 'b']], target: 'b', tokens: viewer },
  { name: 'http success', steps: [base, ['api', 'reply.http_get', { saveAs: 'result' }], ['b', 'reply.chat']], links: [['root', 'next', 'api'], ['api', 'ok', 'b']], target: 'b', tokens: [...viewer, '{local:result}'] },
  { name: 'http failure', steps: [base, ['api', 'reply.http_get', { saveAs: 'result' }], ['b', 'reply.chat']], links: [['root', 'next', 'api'], ['api', 'failure', 'b']], target: 'b', tokens: [...viewer, '{local:error}'] },
  { name: 'http ports merge', steps: [base, ['api', 'reply.http_get', { saveAs: 'result' }], ['b', 'reply.chat']], links: [['root', 'next', 'api'], ['api', 'ok', 'b'], ['api', 'failure', 'b']], target: 'b', tokens: viewer },
  { name: 'loop body', steps: [base, ['loop', 'reply.loop'], ['b', 'reply.chat']], links: [['root', 'next', 'loop'], ['loop', 'body', 'b']], target: 'b', tokens: [...viewer, '{local:index}'] },
  { name: 'loop done', steps: [base, ['loop', 'reply.loop'], ['b', 'reply.chat']], links: [['root', 'next', 'loop'], ['loop', 'done', 'b']], target: 'b', tokens: viewer },
  { name: 'disconnected producer', steps: [['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.chat']], links: [['a', 'next', 'b']], target: 'b', tokens: [] },
  { name: 'disabled producer', steps: [base, ['a', 'reply.math', { saveAs: 'points', disabled: true }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: viewer },
  { name: 'trigger itself', steps: [base], links: [], target: 'root', tokens: [] }
];

test.each(cases)('scope: $name', ({ steps, links, target, tokens }) => {
  const flow: Flow = {
    ...newFlow('Scope'),
    nodes: steps.map(([id, type, config = {}]) => ({ id, type, config, x: 0, y: 0 })),
    edges: links.map(([from, port, to], index) => ({ id: String(index), from, port, to }))
  };
  const before = JSON.stringify(flow);
  expect(variablesFor(flow, target).map((variable) => variable.token)).toEqual(tokens);
  expect(JSON.stringify(flow)).toBe(before);
});
