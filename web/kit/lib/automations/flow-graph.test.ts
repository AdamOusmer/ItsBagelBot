// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { definitionToFlow, flowToDefinition, locateIssues, type Flow, type FlowConfig, type FlowEdge, type FlowNode } from './flow-graph';
import { classifyFlow } from './flow-layer';
import { validateDefinition } from './validate';
import type { Definition } from './types';

const node = (id: FlowNode['id'], type: FlowNode['type'], config: FlowConfig = {}): FlowNode => ({ id, type, x: 0, y: 0, config });
const edge = (from: FlowEdge['from'], port: FlowEdge['port'], to: FlowEdge['to']): FlowEdge => ({ id: `${from}-${port}-${to}`, from, port, to });
const flow = (nodes: FlowNode[], edges: FlowEdge[]): Flow => ({ id: 'f', name: 'flow', enabled: true, nodes, edges });
const meta = { id: 'f', name: 'flow', enabled: true };
type Label = string;

const chat = (text: Label) => ({ type: 'chat', params: { text } });

function lowered(input: Flow): Definition {
  const result = flowToDefinition(input);
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

function rebuilt(definition: Definition): Flow {
  const result = definitionToFlow(definition, meta);
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

const issueCodes = (input: Flow) => {
  const result = flowToDefinition(input);
  return result.ok ? [] : result.issues.map((i) => i.code);
};

// Node and edge shapes of DEMO_SYSTEMS in the builder's demo.ts at the time of writing.
const DEMO = {
  hug: flow(
    [
      node('a1', 'twitch.chat.command', { trigger: '!hug' }),
      node('a2', 'cond.chance', { percent: 85 }),
      node('a3', 'reply.chat', { message: '{user} gives {touser} a big warm hug.' }),
      node('a4', 'reply.chat', { message: 'The hug slipped away, {user}. Try again!' })
    ],
    [edge('a1', 'next', 'a2'), edge('a2', 'yes', 'a3'), edge('a2', 'no', 'a4')]
  ),
  follow: flow(
    [
      node('f1', 'twitch.follow'),
      node('f2', 'reply.chat', { message: 'Welcome to the stream, {user}!' }),
      node('f3', 'reply.points', { amount: 100 })
    ],
    [edge('f1', 'next', 'f2'), edge('f1', 'next', 'f3')]
  ),
  sub: flow(
    [
      node('s1', 'twitch.sub', { variant: 'resub' }),
      node('s2', 'cond.compare', { left: '{months}', op: 'gte', right: '12' }),
      node('s3', 'reply.chat', { message: '{user} has been here {months} months. Legend!' }),
      node('s4', 'reply.chat', { message: 'Thanks for the sub, {user}!' })
    ],
    [edge('s1', 'next', 's2'), edge('s2', 'yes', 's3'), edge('s2', 'no', 's4')]
  ),
  raid: flow(
    [
      node('r1', 'twitch.raid', { minViewers: 1 }),
      node('r2', 'cond.compare', { left: '{viewers}', op: 'gte', right: '5' }),
      node('r3', 'reply.chat', { message: 'Welcome raiders! {user} brought {viewers} friends.' })
    ],
    [edge('r1', 'next', 'r2'), edge('r2', 'yes', 'r3')]
  ),
  hydrate: flow(
    [
      node('h1', 'twitch.reward', { reward: 'hydrate' }),
      node('h2', 'reply.chat', { message: 'Sip time, {user}! Water break for everyone.' }),
      node('h3', 'reply.wait', { seconds: 8 }),
      node('h4', 'reply.chat', { message: 'Did everyone drink? Thanks {user}!' })
    ],
    [edge('h1', 'next', 'h2'), edge('h2', 'next', 'h3'), edge('h3', 'next', 'h4')]
  ),
  cheer: flow(
    [
      node('n1', 'twitch.cheer', { minBits: 100 }),
      node('n2', 'cond.compare', { left: '{bits}', op: 'gte', right: '1000' }),
      node('n3', 'reply.chat', { message: 'HUGE thanks {user} for {bits} bits!' }),
      node('n4', 'reply.lights', { device: 'AB:CD:EF:12:34:56', sku: 'H6159', deviceName: 'Desk strip', mode: 'color', color: '#f5a524' }),
      node('n5', 'reply.chat', { message: 'Thanks for the bits, {user}!' })
    ],
    [edge('n1', 'next', 'n2'), edge('n2', 'yes', 'n3'), edge('n2', 'yes', 'n4'), edge('n2', 'no', 'n5')]
  )
};

describe('demo flow shapes', () => {
  test.each(Object.keys(DEMO) as (keyof typeof DEMO)[])('%s converts as drawn and keeps its shape', (name) => {
    const definition = lowered(DEMO[name]);
    expect(validateDefinition(definition)).toEqual([]);
    const again = rebuilt(definition);
    expect(lowered(again)).toEqual(definition);
    expect(again.nodes.map((n) => n.type).sort()).toEqual(DEMO[name].nodes.map((n) => n.type).sort());
  });

  test('a month count outside a resub is refused', () => {
    const { sub } = DEMO;
    const drawn = { ...sub, nodes: sub.nodes.map((n) => (n.type === 'twitch.sub' ? { ...n, config: { variant: 'any' } } : n)) };
    expect(issueCodes(drawn)).toEqual(['unmappable_token', 'unmappable_token']);
  });
});

describe('lowering rules', () => {
  const trigger = node('t', 'twitch.follow');
  const role = node('g', 'cond.role', { level: 'mod' });

  test('a leading gate with no blocked edge becomes a top-level condition', () => {
    const definition = lowered(flow([trigger, role, node('a', 'reply.chat', { message: 'x' })], [edge('t', 'next', 'g'), edge('g', 'yes', 'a')]));
    expect(definition.conditions).toEqual([{ type: 'perm', params: { level: 'mod' } }]);
    expect(definition.actions).toEqual([chat('x')]);
  });

  test('a gate with a blocked edge becomes if', () => {
    const definition = lowered(
      flow(
        [trigger, role, node('a', 'reply.chat', { message: 'x' }), node('b', 'reply.chat', { message: 'y' })],
        [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', 'no', 'b')]
      )
    );
    expect(definition.conditions).toBeUndefined();
    expect(definition.actions).toEqual([
      { type: 'if', params: { condition: { type: 'perm', params: { level: 'mod' } }, then: [chat('x')], else: [chat('y')] } }
    ]);
  });

  test('a gate after an action is an if even with no blocked edge', () => {
    const definition = lowered(
      flow(
        [trigger, node('a', 'reply.chat', { message: 'x' }), role, node('b', 'reply.chat', { message: 'y' })],
        [edge('t', 'next', 'a'), edge('a', 'next', 'g'), edge('g', 'yes', 'b')]
      )
    );
    expect(definition.actions).toEqual([
      chat('x'),
      { type: 'if', params: { condition: { type: 'perm', params: { level: 'mod' } }, then: [chat('y')] } }
    ]);
  });

  test('fan-out lowers to parallel paths in edge order', () => {
    const definition = lowered(
      flow(
        [trigger, node('b', 'reply.chat', { message: 'second' }), node('a', 'reply.chat', { message: 'first' })],
        [edge('t', 'next', 'a'), edge('t', 'next', 'b')]
      )
    );
    expect(definition.actions).toEqual([
      { type: 'parallel', params: { branches: [{ weight: 1, actions: [chat('first')] }, { weight: 1, actions: [chat('second')] }] } }
    ]);
  });

  test('a clean merge lowers to if then the shared step and rebuilds as a join', () => {
    const merged = flow(
      [trigger, role, node('a', 'reply.chat', { message: 'yes' }), node('b', 'reply.chat', { message: 'no' }), node('s', 'reply.chat', { message: 'shared' })],
      [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', 'no', 'b'), edge('a', 'next', 's'), edge('b', 'next', 's')]
    );
    const definition = lowered(merged);
    expect(definition.actions).toEqual([
      { type: 'if', params: { condition: { type: 'perm', params: { level: 'mod' } }, then: [chat('yes')], else: [chat('no')] } },
      chat('shared')
    ]);
    const again = rebuilt(definition);
    const shared = again.nodes.find((n) => n.config.message === 'shared')!;
    expect(again.edges.filter((e) => e.to === shared.id)).toHaveLength(2);
    expect(lowered(again)).toEqual(definition);
  });

  test('a branch joining directly from the blocked port keeps an empty else', () => {
    const definition = lowered(
      flow(
        [trigger, role, node('a', 'reply.chat', { message: 'yes' }), node('s', 'reply.chat', { message: 'shared' })],
        [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', 'no', 's'), edge('a', 'next', 's')]
      )
    );
    expect(definition.actions).toEqual([
      { type: 'if', params: { condition: { type: 'perm', params: { level: 'mod' } }, then: [chat('yes')] } },
      chat('shared')
    ]);
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test('thresholds and sub variants survive the round trip', () => {
    const cheer = lowered(flow([node('t', 'twitch.cheer', { minBits: 100 })], []));
    expect(cheer.trigger).toEqual({ type: 'event', params: { kind: 'channel.cheer', filters: { amount: '100' } } });
    expect(rebuilt(cheer).nodes[0]!.config).toEqual({ minBits: 100 });

    const resub = lowered(flow([node('t', 'twitch.sub', { variant: 'resub' })], []));
    expect(rebuilt(resub).nodes[0]!.config).toEqual({ variant: 'resub' });
    const fresh = lowered(flow([node('t', 'twitch.sub', { variant: 'new' })], []));
    expect(rebuilt(fresh).nodes[0]!.config).toEqual({ variant: 'new' });
  });
});

describe('reply tokens', () => {
  const line = (message: Label) => node('a', 'reply.chat', { message });
  const after = (trigger: FlowNode, message: Label) => flow([trigger, line(message)], [edge(trigger.id, 'next', 'a')]);
  const text = (definition: Definition) => definition.actions?.[0]?.params?.text;

  test.each([
    ['twitch.cheer', {}, '{bits} from {user}', '{event.amount} from {user}'],
    ['twitch.raid', {}, '{viewers} with {user.id}', '{event.viewers} with {user.id}'],
    ['twitch.sub', { variant: 'any' }, 'tier {tier}', 'tier {event.tier}'],
    ['twitch.sub', { variant: 'resub' }, '{months} at {tier}', '{event.months} at {event.tier}'],
    ['twitch.sub', { variant: 'gift' }, '{count} at {tier}', '{event.amount} at {event.tier}'],
    ['twitch.reward', { reward: 'r' }, 'said {input}', 'said {event.input}'],
    ['twitch.stream.online', {}, '{title} in {game}', '{title} in {game}'],
    ['twitch.chat.command', { trigger: '!hug' }, '{user} hugs {touser} {args}', '{user} hugs {touser} {args}']
  ] as [string, FlowConfig, string, string][])('%s %j writes %s as %s and reads it back', (type, config, drawn, stored) => {
    const definition = lowered(after(node('t', type, config), drawn));
    expect(text(definition)).toBe(stored);
    const again = rebuilt(definition);
    expect(again.nodes[1]!.config.message).toBe(drawn);
    expect(lowered(again)).toEqual(definition);
  });

  test.each([
    ['twitch.sub', { variant: 'any' }, '{months}'],
    ['twitch.sub', { variant: 'new' }, '{months}'],
    ['twitch.sub', { variant: 'gift' }, '{months}'],
    ['twitch.follow', {}, '{bits}'],
    ['twitch.follow', {}, '{count}'],
    ['twitch.raid', {}, '{count}']
  ] as [string, FlowConfig, string][])('%s %j cannot carry %s', (type, config, token) => {
    const result = flowToDefinition(after(node('t', type, config), `x ${token}`));
    expect(result).toEqual({
      ok: false,
      issues: [{ path: `nodes.a.message.${token.slice(1, -1)}`, code: 'unmappable_token' }]
    });
  });

  const bump = (id: FlowNode['id'], counter: Label) => node(id, 'reply.counter', { counter });

  test('{count} after one counter reads that counter and draws back as {count}', () => {
    const definition = lowered(
      flow([node('t', 'twitch.chat.command', { trigger: '!hug' }), bump('c', 'hugs'), line('hugs: {count}')], [edge('t', 'next', 'c'), edge('c', 'next', 'a')])
    );
    expect(definition.actions?.[1]?.params?.text).toBe('hugs: {counter:hugs}');
    expect(rebuilt(definition).nodes[2]!.config.message).toBe('hugs: {count}');
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test('{count} after two different counters is ambiguous', () => {
    const input = flow(
      [node('t', 'twitch.follow'), bump('c', 'hugs'), bump('d', 'deaths'), line('{count}')],
      [edge('t', 'next', 'c'), edge('c', 'next', 'd'), edge('d', 'next', 'a')]
    );
    expect(issueCodes(input)).toEqual(['unmappable_token']);
  });

  test('a counter before a line does not count from a sibling branch', () => {
    const input = flow(
      [node('t', 'twitch.follow'), bump('c', 'hugs'), line('{count}')],
      [edge('t', 'next', 'a'), edge('t', 'next', 'c')]
    );
    expect(issueCodes(input)).toEqual(['unmappable_token']);
  });

  test('a gift count next to a counter is refused rather than guessed', () => {
    const input = flow(
      [node('t', 'twitch.sub', { variant: 'gift' }), bump('c', 'hugs'), line('{count}')],
      [edge('t', 'next', 'c'), edge('c', 'next', 'a')]
    );
    expect(issueCodes(input)).toEqual(['unmappable_token']);
  });

  test('{count} on a bare command stays, it means uses in the command row', () => {
    expect(text(lowered(after(node('t', 'twitch.chat.command', { trigger: '!hug' }), 'used {count} times')))).toBe('used {count} times');
  });

  test('catalog tokens and unknown event fields pass through', () => {
    const definition = lowered(after(node('t', 'twitch.cheer'), '{uptime} {event.message}'));
    expect(text(definition)).toBe('{uptime} {event.message}');
    expect(rebuilt(definition).nodes[1]!.config.message).toBe('{uptime} {event.message}');
  });
});

describe('refusals', () => {
  const chatNode = (id: FlowNode['id']) => node(id, 'reply.chat', { message: id });

  test('no trigger', () => {
    expect(issueCodes(flow([chatNode('a')], []))).toEqual(['no_trigger']);
  });

  test('multiple triggers', () => {
    expect(issueCodes(flow([node('t', 'twitch.follow'), node('u', 'twitch.raid')], []))).toEqual(['multiple_triggers', 'multiple_triggers']);
  });

  test('cycle', () => {
    const input = flow([node('t', 'twitch.follow'), chatNode('a'), chatNode('b')], [edge('t', 'next', 'a'), edge('a', 'next', 'b'), edge('b', 'next', 'a')]);
    expect(issueCodes(input)).toEqual(['cycle']);
  });

  test('unconnected node', () => {
    const result = flowToDefinition(flow([node('t', 'twitch.follow'), chatNode('a'), chatNode('b')], [edge('t', 'next', 'a')]));
    expect(result).toEqual({ ok: false, issues: [{ path: 'nodes.b', code: 'unconnected_node' }] });
  });

  test('edge to a missing node', () => {
    expect(issueCodes(flow([node('t', 'twitch.follow')], [edge('t', 'next', 'ghost')]))).toEqual(['unconnected_node']);
  });

  test('a merge that only some branches share is unstructured', () => {
    const input = flow(
      [node('t', 'twitch.follow'), node('g', 'cond.role', { level: 'mod' }), chatNode('a'), chatNode('b'), chatNode('c'), chatNode('s')],
      [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', 'yes', 'c'), edge('g', 'no', 'b'), edge('a', 'next', 's'), edge('b', 'next', 's')]
    );
    expect(issueCodes(input)).toEqual(['unstructured_merge']);
  });

  test('a step after a pausing fork is refused, since it would not wait for that path', () => {
    const drawn = flow(
      [node('t', 'twitch.follow'), node('w', 'reply.wait', { seconds: 5 }), node('a', 'reply.chat', { message: 'a' }), node('j', 'reply.chat', { message: 'j' })],
      [edge('t', 'next', 'w'), edge('t', 'next', 'a'), edge('w', 'next', 'j'), edge('a', 'next', 'j')]
    );
    expect(issueCodes(drawn)).toEqual(['unstructured_merge']);
  });

  test('a fork saves as parallel paths and reopens as a fork', () => {
    const drawn = flow(
      [node('t', 'twitch.follow'), node('w', 'reply.wait', { seconds: 5 }), node('l', 'reply.chat', { message: 'late' }), node('n', 'reply.chat', { message: 'now' })],
      [edge('t', 'next', 'w'), edge('w', 'next', 'l'), edge('t', 'next', 'n')]
    );
    const lowered = flowToDefinition(drawn);
    if (!lowered.ok) throw new Error('refused');
    expect(lowered.value.actions).toEqual([
      {
        type: 'parallel',
        params: {
          branches: [
            { weight: 1, actions: [{ type: 'delay', params: { ms: 5000 } }, { type: 'chat', params: { text: 'late' } }] },
            { weight: 1, actions: [{ type: 'chat', params: { text: 'now' } }] }
          ]
        }
      }
    ]);
    expect(validateDefinition(lowered.value)).toEqual([]);
    const reopened = definitionToFlow(lowered.value, { id: 'x', name: 'x', enabled: true });
    if (!reopened.ok) throw new Error('did not reopen');
    const triggerId = reopened.value.nodes.find((n) => n.type === 'twitch.follow')!.id;
    expect(reopened.value.edges.filter((e) => e.from === triggerId)).toHaveLength(2);
  });

  test('a wait over the delay budget', () => {
    expect(issueCodes(flow([node('t', 'twitch.follow'), node('w', 'reply.wait', { seconds: 61 })], [edge('t', 'next', 'w')]))).toEqual(['wait_too_long']);
  });
});

describe('chat word trigger', () => {
  const wordFlow = (word: Label) => flow([node('t', 'twitch.chat.word', { word }), node('r', 'reply.chat', { message: 'hi' })], [edge('t', 'next', 'r')]);
  const pattern = (word: Label) => lowered(wordFlow(word)).trigger.params!.name as string;
  const fires = (word: Label, text: Label) => new RegExp(pattern(word), 'iu').test(text);

  test.each(['bagel', 'café', 'hi!', 'good morning', 'a.b*c', '(x)', '[y]', '{z}', '^$|', 'c\\1'])('%s validates and round trips', (word) => {
    const definition = lowered(wordFlow(word));
    expect(definition.trigger.params!.match).toBe('regex');
    expect(validateDefinition(definition)).toEqual([]);
    expect(rebuilt(definition).nodes[0]).toMatchObject({ type: 'twitch.chat.word', config: { word } });
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test.each([
    ['c\\1', 'say c\\1 now', true],
    ['c\\1', 'say c1 now', false],
    ['hi', 'hi there', true],
    ['hi', 'oh hi', true],
    ['hi', 'HI!', true],
    ['hi', 'this', false],
    ['hi', 'hi_there', false],
    ['café', 'un café svp', true],
    ['caf', 'un café svp', false],
    ['hi!', 'well hi! there', true],
    ['a.b', 'axb', false]
  ])('word %s on "%s" fires: %s', (word, text, expected) => {
    expect(fires(word, text)).toBe(expected);
  });

  test('a blank word is refused by validation, it would otherwise fire on every line', () => {
    expect(validateDefinition(lowered(wordFlow('  '))).length).toBeGreaterThan(0);
  });

  test('it never reads as a plain command', () => {
    expect(classifyFlow(lowered(wordFlow('bagel')))).toEqual({ layer: 'automation' });
  });

  test.each(['h.g', '\\bhi\\b', '(?:^|[^\\p{L}\\p{N}_])a.b(?:$|[^\\p{L}\\p{N}_])'])('hand written regex %s is not drawn as a word', (name) => {
    const result = definitionToFlow({ version: 1, trigger: { type: 'command', params: { name, match: 'regex' } }, actions: [chat('a')] }, meta);
    expect(result).toEqual({ ok: false, issues: [{ path: 'trigger', code: 'unsupported_trigger' }] });
  });
});

describe('specific user condition', () => {
  const trigger = node('t', 'twitch.follow');
  const gate = node('u', 'cond.user', { login: 'itsbagelbot', userId: '123456' });
  const yes = node('y', 'reply.chat', { message: 'yes' });
  const no = node('n', 'reply.chat', { message: 'no' });
  const users = { type: 'users', params: { allow_ids: ['123456'] } };

  test.each([
    ['no id', { login: 'itsbagelbot' }],
    ['an empty id', { login: 'itsbagelbot', userId: '' }]
  ])('%s is an invalid step that names the node', (_label, config) => {
    const result = flowToDefinition(flow([trigger, node('u', 'cond.user', config), yes], [edge('t', 'next', 'u'), edge('u', 'yes', 'y')]));
    expect(result).toEqual({ ok: false, issues: [{ path: 'nodes.u', code: 'user_not_resolved' }] });
  });

  test('a branching step without an id is refused too', () => {
    const result = flowToDefinition(flow([trigger, node('u', 'cond.user', { login: 'x' }), yes, no], [edge('t', 'next', 'u'), edge('u', 'yes', 'y'), edge('u', 'no', 'n')]));
    expect(result).toEqual({ ok: false, issues: [{ path: 'nodes.u', code: 'user_not_resolved' }] });
  });

  test('the id comes back, the display login has nowhere to live in the definition', () => {
    const definition = lowered(flow([trigger, gate, yes], [edge('t', 'next', 'u'), edge('u', 'yes', 'y')]));
    expect(JSON.stringify(definition)).not.toContain('itsbagelbot');
    expect(rebuilt(definition).nodes[1]).toMatchObject({ type: 'cond.user', config: { userId: '123456' } });
    expect(rebuilt(definition).nodes[1]!.config.login).toBeUndefined();
  });

  test.each([
    ['letters', ['abc']],
    ['a login style value', ['itsbagelbot']],
    ['an empty entry', ['']]
  ])('validation refuses %s as an id', (_label, ids) => {
    const definition: Definition = { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, conditions: [{ type: 'users', params: { allow_ids: ids } }], actions: [chat('a')] };
    expect(validateDefinition(definition).length).toBeGreaterThan(0);
  });

  test('with no other branch it gates the whole flow', () => {
    const definition = lowered(flow([trigger, gate, yes], [edge('t', 'next', 'u'), edge('u', 'yes', 'y')]));
    expect(definition.conditions).toEqual([users]);
    expect(validateDefinition(definition)).toEqual([]);
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test('with both ports it branches and round trips', () => {
    const definition = lowered(flow([trigger, gate, yes, no], [edge('t', 'next', 'u'), edge('u', 'yes', 'y'), edge('u', 'no', 'n')]));
    expect(definition.actions).toEqual([{ type: 'if', params: { condition: users, then: [chat('yes')], else: [chat('no')] } }]);
    expect(validateDefinition(definition)).toEqual([]);
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test('a command gated on one user stays an automation', () => {
    const definition = lowered(flow([node('t', 'twitch.chat.command', { trigger: '!hug' }), gate, yes], [edge('t', 'next', 'u'), edge('u', 'yes', 'y')]));
    expect(classifyFlow(definition)).toEqual({ layer: 'automation' });
  });

  test.each([
    ['several allowed', { allow_ids: ['1', '2'] }],
    ['an id deny list', { allow_ids: ['1'], deny_ids: ['2'] }],
    ['a login list beside the id', { allow_ids: ['1'], allow: ['a'] }],
    ['only logins', { allow: ['a'] }],
    ['only a deny list', { deny: ['b'] }]
  ])('%s is not drawn as one user', (_label, params) => {
    const result = definitionToFlow({ version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, conditions: [{ type: 'users', params }], actions: [chat('a')] }, meta);
    expect(result).toEqual({ ok: false, issues: [{ path: 'conditions[0]', code: 'unsupported_condition' }] });
  });
});

describe('set lights step', () => {
  const trigger = node('t', 'twitch.follow');
  const withStep = (step: FlowNode) => flow([trigger, step], [edge('t', 'next', step.id)]);
  const color = node('l', 'reply.lights', { device: 'AA:BB', sku: 'H6008', deviceName: 'Desk', mode: 'color', color: '#f5a524' });
  const scene = node('l', 'reply.lights', {
    device: 'AA:BB',
    sku: 'H6008',
    deviceName: 'Desk',
    mode: 'scene',
    scene: 'snapshot:4:9',
    sceneName: 'Sunset',
    sceneType: 'devices.capabilities.dynamic_scene',
    sceneInstance: 'snapshot',
    sceneValue: '{"paramId":4,"id":9}'
  });

  test('a colour is carried with the device, model and name', () => {
    expect(lowered(withStep(color)).actions).toEqual([
      { type: 'lights', params: { device: 'AA:BB', sku: 'H6008', device_name: 'Desk', mode: 'color', color: '#f5a524' } }
    ]);
  });

  test('a scene carries what the lights service needs to set it', () => {
    const definition = lowered(withStep(scene));
    expect(definition.actions).toEqual([
      {
        type: 'lights',
        params: {
          device: 'AA:BB',
          sku: 'H6008',
          device_name: 'Desk',
          mode: 'scene',
          scene_id: 'snapshot:4:9',
          scene_type: 'devices.capabilities.dynamic_scene',
          scene_instance: 'snapshot',
          scene_value: '{"paramId":4,"id":9}',
          scene_name: 'Sunset'
        }
      }
    ]);
    expect(validateDefinition(definition)).toEqual([]);
  });

  test.each([color, scene])('$config.mode reopens as it was saved', (step) => {
    expect(rebuilt(lowered(withStep(step))).nodes[1]).toMatchObject({ type: 'reply.lights', config: step.config });
  });

  test('a colour left on the picker default saves white', () => {
    const untouched = node('l', 'reply.lights', { device: 'AA:BB', sku: 'H6008' });
    expect(lowered(withStep(untouched)).actions?.[0]?.params).toMatchObject({ mode: 'color', color: '#ffffff' });
  });

  test('a scene mode with no scene picked is refused by validation, not saved half empty', () => {
    const stale = node('l', 'reply.lights', { device: 'AA:BB', sku: 'H6008', mode: 'scene', sceneType: 'x', sceneInstance: 'y', sceneValue: '1' });
    const issues = validateDefinition(lowered(withStep(stale)));
    expect(issues.map((i) => i.path)).toEqual(['actions[0].params.scene_instance', 'actions[0].params.scene_type', 'actions[0].params.scene_value']);
  });
});

describe('locating validation issues on steps', () => {
  const trigger = node('t', 'twitch.raid', { minViewers: 5 });
  const gate = node('g', 'cond.role', { level: 'mod' });
  const check = node('c', 'cond.compare', { left: '', op: 'gte', right: '5' });
  const post = node('d', 'reply.lights', { sku: 'H6008', mode: 'color', color: '#f5a524' });
  const wait = (id: FlowNode['id']) => node(id, 'reply.wait', { seconds: 31 });

  const refused = (input: Flow) => {
    const result = flowToDefinition(input);
    if (!result.ok) throw new Error(JSON.stringify(result.issues));
    return locateIssues(input, validateDefinition(result.value));
  };

  test('a leading gate is found by its top-level condition', () => {
    const drawn = flow([trigger, check, node('a', 'reply.chat', { message: 'x' })], [edge('t', 'next', 'c'), edge('c', 'yes', 'a')]);
    expect(refused(drawn)).toEqual([{ path: 'nodes.c.params.left', code: 'required' }]);
  });

  test('a step inside a branch is found by its nested path', () => {
    const drawn = flow([trigger, gate, post, node('n', 'reply.chat', { message: 'no' })], [edge('t', 'next', 'g'), edge('g', 'yes', 'd'), edge('g', 'no', 'n')]);
    expect(refused(drawn)).toEqual([{ path: 'nodes.d.params.device', code: 'required' }]);
  });

  test('the condition of a branch is found through the if step', () => {
    const drawn = flow(
      [trigger, node('a', 'reply.chat', { message: 'x' }), check, node('b', 'reply.chat', { message: 'y' })],
      [edge('t', 'next', 'a'), edge('a', 'next', 'c'), edge('c', 'yes', 'b')]
    );
    expect(refused(drawn)).toEqual([{ path: 'nodes.c.params.left', code: 'required' }]);
  });

  test('waits that add up past the run budget point at the last wait', () => {
    const drawn = flow([trigger, wait('w1'), wait('w2')], [edge('t', 'next', 'w1'), edge('w1', 'next', 'w2')]);
    expect(refused(drawn)).toEqual([{ path: 'nodes.w2', code: 'delay_budget' }]);
  });

  test('an issue with no step behind it is left alone', () => {
    const drawn = flow([trigger, node('a', 'reply.chat', { message: 'x' })], [edge('t', 'next', 'a')]);
    expect(locateIssues(drawn, [{ path: 'trigger.params.kind', code: 'required' }, { path: 'name', code: 'required' }])).toEqual([
      { path: 'trigger.params.kind', code: 'required' },
      { path: 'name', code: 'required' }
    ]);
  });
});

describe('compare value step', () => {
  const trigger = node('t', 'twitch.cheer', { minBits: 100 });
  const compare = node('c', 'cond.compare', { left: '{bits}', op: 'gte', right: '1000' });
  const yes = node('y', 'reply.chat', { message: 'big {bits}' });
  const no = node('n', 'reply.chat', { message: 'small' });

  test('as the first gate it becomes a condition on the event field', () => {
    const definition = lowered(flow([trigger, compare, yes], [edge('t', 'next', 'c'), edge('c', 'yes', 'y')]));
    expect(definition.conditions).toEqual([{ type: 'compare', params: { left: '{event.amount}', op: 'gte', right: '1000' } }]);
    expect(validateDefinition(definition)).toEqual([]);
    expect(rebuilt(definition).nodes[1]).toMatchObject({ type: 'cond.compare', config: compare.config });
  });

  test('with both ports it branches and round trips', () => {
    const drawn = flow([trigger, compare, yes, no], [edge('t', 'next', 'c'), edge('c', 'yes', 'y'), edge('c', 'no', 'n')]);
    const definition = lowered(drawn);
    expect(definition.actions?.[0]).toMatchObject({ type: 'if', params: { condition: { type: 'compare' } } });
    expect(validateDefinition(definition)).toEqual([]);
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test('an operator left on the select default is saved as at least', () => {
    const untouched = node('c', 'cond.compare', { left: '{bits}', right: '5' });
    const definition = lowered(flow([trigger, untouched, yes], [edge('t', 'next', 'c'), edge('c', 'yes', 'y')]));
    expect(definition.conditions).toEqual([{ type: 'compare', params: { left: '{event.amount}', op: 'gte', right: '5' } }]);
  });

  test('a token the trigger cannot supply is refused on the compare step', () => {
    const wrong = node('c', 'cond.compare', { left: '{months}', op: 'gte', right: '5' });
    const result = flowToDefinition(flow([trigger, wrong, yes], [edge('t', 'next', 'c'), edge('c', 'yes', 'y')]));
    expect(result).toEqual({ ok: false, issues: [{ path: 'nodes.c.left.months', code: 'unmappable_token' }] });
  });

  test('a stored operator the builder has no choice for is not drawn', () => {
    const stored: Definition = {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.follow' } },
      conditions: [{ type: 'compare', params: { left: '{user}', op: 'gt', right: 'a' } }],
      actions: [chat('a')]
    };
    expect(definitionToFlow(stored, meta)).toEqual({ ok: false, issues: [{ path: 'conditions[0]', code: 'unsupported_condition' }] });
  });
});

describe('reading stored definitions', () => {
  test('a stored Discord post, an action that no longer exists, cannot be drawn', () => {
    const stored: Definition = {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.raid' } },
      actions: [chat('a'), { type: 'discord_post', params: { guild_id: '123456789012345678', channel_id: '223456789012345678', text: 'hi' } }]
    };
    expect(definitionToFlow(stored, meta)).toEqual({ ok: false, issues: [{ path: 'actions[1]', code: 'unsupported_action' }] });
    expect(validateDefinition(stored)).toEqual([{ path: 'actions[1].type', code: 'unknown_type' }]);
  });

  test('a definition the builder cannot draw reports where', () => {
    const result = definitionToFlow(
      { version: 1, trigger: { type: 'manual', params: {} }, actions: [] },
      meta
    );
    expect(result).toEqual({ ok: false, issues: [{ path: 'trigger', code: 'unsupported_trigger' }] });

    const action = definitionToFlow(
      { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, actions: [chat('a'), { type: 'if', params: { condition: { type: 'live', params: { state: 'live' } }, then: [{ type: 'shield', params: { on: true } }] } }] },
      meta
    );
    expect(action).toEqual({ ok: false, issues: [{ path: 'actions[1].condition', code: 'unsupported_condition' }] });
  });

  test('a disabled action stays disabled', () => {
    const definition: Definition = {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.follow' } },
      actions: [{ ...chat('a'), enabled: false }]
    };
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test('aliases survive the round trip', () => {
    const definition: Definition = {
      version: 1,
      trigger: { type: 'command', params: { name: '!hug', aliases: ['!cuddle', '!squeeze'] } },
      actions: [chat('a')]
    };
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });
});
