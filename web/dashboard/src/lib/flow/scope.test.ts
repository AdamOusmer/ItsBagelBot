// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { newFlow, variablesFor, type Flow, type FlowNode } from './model';
import { variableLabel } from './copy';
import { english } from './english.testing';

type Step = [FlowNode['id'], FlowNode['type'], FlowNode['config']?];
type Link = [string, string, string];
type ScopeCase = { name: string; steps: Step[]; links: Link[]; target: string; tokens: string[] };
const viewer = ['{user}', '{user.id}'];
const base: Step = ['root', 'twitch.follow'];

const cases: ScopeCase[] = [
  { name: 'linear ancestors', steps: [base, ['a', 'reply.counter', { counter: 'wins' }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: [...viewer, '{counter:wins}'] },
  { name: 'parallel sibling', steps: [base, ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['root', 'next', 'b']], target: 'b', tokens: viewer },
  { name: 'parallel merge', steps: [base, ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.wait'], ['join', 'reply.chat']], links: [['root', 'next', 'a'], ['root', 'next', 'b'], ['a', 'next', 'join'], ['b', 'next', 'join']], target: 'join', tokens: viewer },
  { name: 'same local name on parallel paths is discarded', steps: [base, ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.math', { saveAs: 'points' }], ['join', 'reply.chat']], links: [['root', 'next', 'a'], ['root', 'next', 'b'], ['a', 'next', 'join'], ['b', 'next', 'join']], target: 'join', tokens: viewer },
  { name: 'parent local survives parallel overwrite', steps: [base, ['prior', 'reply.math', { saveAs: 'points' }], ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.math', { saveAs: 'newvalue' }], ['join', 'reply.chat']], links: [['root', 'next', 'prior'], ['prior', 'next', 'a'], ['prior', 'next', 'b'], ['a', 'next', 'join'], ['b', 'next', 'join']], target: 'join', tokens: [...viewer, '{local:points}'] },
  { name: 'same local on exclusive branches survives', steps: [base, ['gate', 'cond.chance'], ['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.math', { saveAs: 'points' }], ['join', 'reply.chat']], links: [['root', 'next', 'gate'], ['gate', 'yes', 'a'], ['gate', 'no', 'b'], ['a', 'next', 'join'], ['b', 'next', 'join']], target: 'join', tokens: [...viewer, '{local:points}'] },
  { name: 'cooldown rejected port', steps: [base, ['gate', 'cond.cooldown'], ['b', 'reply.chat']], links: [['root', 'next', 'gate'], ['gate', 'no', 'b']], target: 'b', tokens: [...viewer, '{cooldown:left}'] },
  { name: 'cooldown ports merge', steps: [base, ['gate', 'cond.cooldown'], ['b', 'reply.chat']], links: [['root', 'next', 'gate'], ['gate', 'yes', 'b'], ['gate', 'no', 'b']], target: 'b', tokens: viewer },
  { name: 'http success', steps: [base, ['api', 'reply.http_get', { saveAs: 'result' }], ['b', 'reply.chat']], links: [['root', 'next', 'api'], ['api', 'ok', 'b']], target: 'b', tokens: [...viewer, '{local:result}'] },
  { name: 'http failure', steps: [base, ['api', 'reply.http_get', { saveAs: 'result' }], ['b', 'reply.chat']], links: [['root', 'next', 'api'], ['api', 'failure', 'b']], target: 'b', tokens: [...viewer, '{local:error}'] },
  { name: 'http success clears inherited error', steps: [base, ['api', 'reply.http_get'], ['retry', 'reply.http_get', { saveAs: 'result' }], ['b', 'reply.chat']], links: [['root', 'next', 'api'], ['api', 'failure', 'retry'], ['retry', 'ok', 'b']], target: 'b', tokens: [...viewer, '{local:result}'] },
  { name: 'channel variable', steps: [base, ['a', 'reply.var', { do: 'set', name: 'round2', value: '7' }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: [...viewer, '{var:round2}'] },
  { name: 'viewer variable', steps: [base, ['a', 'reply.var', { do: 'add', scope: 'viewer', name: 'hype', amount: 7 }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: [...viewer, '{viewervar:hype}'] },
  { name: 'forget does not introduce a variable', steps: [base, ['a', 'reply.var', { do: 'forget', name: 'round2' }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: viewer },
  { name: 'http ports merge', steps: [base, ['api', 'reply.http_get', { saveAs: 'result' }], ['b', 'reply.chat']], links: [['root', 'next', 'api'], ['api', 'ok', 'b'], ['api', 'failure', 'b']], target: 'b', tokens: viewer },
  { name: 'loop body', steps: [base, ['loop', 'reply.loop'], ['b', 'reply.chat']], links: [['root', 'next', 'loop'], ['loop', 'body', 'b']], target: 'b', tokens: [...viewer, '{local:index}'] },
  { name: 'loop done', steps: [base, ['loop', 'reply.loop'], ['b', 'reply.chat']], links: [['root', 'next', 'loop'], ['loop', 'done', 'b']], target: 'b', tokens: viewer },
  { name: 'disconnected producer', steps: [['a', 'reply.math', { saveAs: 'points' }], ['b', 'reply.chat']], links: [['a', 'next', 'b']], target: 'b', tokens: [] },
  { name: 'disabled producer', steps: [base, ['a', 'reply.math', { saveAs: 'points', disabled: true }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: viewer },
  { name: 'trigger itself', steps: [base], links: [], target: 'root', tokens: [] },
  { name: 'cooldown extension', steps: [base, ['a', 'reply.cooldown_add', { name: 'break', seconds: 60 }], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'next', 'b']], target: 'b', tokens: [...viewer, '{cooldown:left}'] },
  { name: 'enough points', steps: [base, ['a', 'reply.points_spend'], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'enough', 'b']], target: 'b', tokens: [...viewer, '{points}'] },
  { name: 'not enough points', steps: [base, ['a', 'reply.points_spend'], ['b', 'reply.chat']], links: [['root', 'next', 'a'], ['a', 'notEnough', 'b']], target: 'b', tokens: [...viewer, '{points}'] }
];

const mixedSteps: Step[] = [base, ['chance', 'cond.chance'], ['math', 'reply.math', { saveAs: 'points' }], ['a', 'reply.chat'], ['b', 'reply.chat'], ['join', 'reply.chat']];
const mixedLinks: Link[] = [['root', 'next', 'chance'], ['chance', 'yes', 'math'], ['math', 'next', 'a'], ['math', 'next', 'b'], ['a', 'next', 'join'], ['b', 'next', 'join'], ['chance', 'no', 'join']];
for (const reversed of [false, true]) cases.push({ name: `conditional bypass of parallel fork, reversed ${reversed}`, steps: mixedSteps, links: reversed ? mixedLinks.toReversed() : mixedLinks, target: 'join', tokens: viewer });
const forkedBypass: Link[] = [...mixedLinks.filter(([from, port]) => from !== 'chance' || port !== 'no'), ['chance', 'no', 'c'], ['chance', 'no', 'd'], ['c', 'next', 'join'], ['d', 'next', 'join']];
for (const reversed of [false, true]) cases.push({ name: `conditional bypass has its own fork, reversed ${reversed}`, steps: [...mixedSteps, ['c', 'reply.chat'], ['d', 'reply.chat']], links: reversed ? forkedBypass.toReversed() : forkedBypass, target: 'join', tokens: viewer });
for (const target of ['root', 'loop', 'join', 'outside']) cases.push({
  name: `persistent variables are offered at ${target}`,
  steps: [base, ['loop', 'reply.loop'], ['add', 'reply.var', { do: 'add', name: 'hype', amount: 3 }], ['join', 'reply.chat'], ['outside', 'reply.chat']],
  links: [['root', 'next', 'loop'], ['loop', 'body', 'add'], ['loop', 'done', 'join']], target,
  tokens: [...(target === 'loop' || target === 'join' ? viewer : []), '{var:hype}']
});

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

test.each<Step>([
  ['saved', 'reply.math', { saveAs: 'joke', expression: '6 * 7' }],
  ['saved', 'reply.var', { do: 'set', name: 'joke', value: '42' }],
  ['saved', 'reply.var', { do: 'add', name: 'joke', scope: 'viewer', amount: 42 }]
])('%s %s labels its own value', (id, type, config) => {
  const flow: Flow = { ...newFlow('Names'), nodes: [
    { id: 'root', type: 'twitch.follow', config: {}, x: 0, y: 0 },
    { id, type, config: config ?? {}, x: 0, y: 0 },
    { id: 'chat', type: 'reply.chat', config: {}, x: 0, y: 0 }
  ], edges: [
    { id: 'a', from: 'root', port: 'next', to: id }, { id: 'b', from: id, port: 'next', to: 'chat' }
  ] };
  const variable = variablesFor(flow, 'chat').at(-1)!;
  expect([variableLabel(english, variable), variable.sample]).toEqual(['joke', '42']);
});
