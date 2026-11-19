// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { definitionToFlow, flowStepRefs, flowToDefinition, locateIssues, type Flow, type FlowConfig, type FlowEdge, type FlowNode } from './flow-graph';
import { classifyFlow } from './flow-layer';
import { SETUP_LOGIN, SETUP_REWARD_ID } from './flow-setup';
import { validateDefinition } from './validate';
import type { Action, Condition, Definition, Params, Trigger } from './types';

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

const sharedNested = JSON.parse(readFileSync(new URL('../../../../internal/domain/automation/testdata/valid/nested_and_disabled.json', import.meta.url), 'utf8')) as Definition;

const controlRoundtrips: { name: Label; type: FlowNode['type']; actions: Action[]; config: FlowConfig; ports: FlowEdge['port'][] }[] = [
  { name: 'terminal stop', type: 'reply.stop', actions: [chat('before'), { type: 'stop' }], config: {}, ports: [] },
  {
    name: 'random with an empty path and a shared continuation', type: 'reply.random', config: { paths: '3', weight1: 1, weight2: 2, weight3: 7 },
    actions: [{ type: 'random_pick', params: { branches: [{ weight: 1, actions: [chat('a')] }, { weight: 2, actions: [] }, { weight: 7, actions: [chat('c')] }] } }, chat('after')],
    ports: ['p1', 'p2', 'p3']
  },
  { name: 'recipe call', type: 'reply.run', actions: [{ type: 'run', params: { automation_id: 'helper' } }], config: { automationId: 'helper' }, ports: [] }
];

test.each(controlRoundtrips)('$name roundtrips through the graph', ({ type, actions, config, ports }) => {
  const definition: Definition = { version: 1, trigger: { type: 'command', params: { name: '!test' } }, actions };
  const reopened = rebuilt(definition);
  const node = reopened.nodes.find((entry) => entry.type === type);
  const edges = reopened.edges.filter((edge) => edge.from === node?.id).map((edge) => edge.port).sort();
  expect({ config: node?.config, ports: edges, definition: lowered(reopened), issues: validateDefinition(definition) })
    .toEqual({ config, ports, definition, issues: [] });
});

test('imports the shared random and stop fixture without unsupported actions', () => {
  const reopened = rebuilt({ ...sharedNested, actions: [sharedNested.actions![1]!] });
  expect(reopened.nodes.slice(2).map(({ type, config }) => ({ type, config }))).toEqual([
    { type: 'reply.random', config: { paths: '2', weight1: 1, weight2: 3 } },
    { type: 'reply.chat', config: { message: 'hello {user}' } },
    { type: 'reply.stop', config: {} }
  ]);
});

function cooldownFlow(trigger: FlowNode, config: FlowConfig): Flow {
  const gate = node('g', 'cond.cooldown', config);
  const reply = node('a', 'reply.chat', { message: 'x' });
  return flow([trigger, gate, reply], [edge(trigger.id, 'next', 'g'), edge('g', 'yes', 'a')]);
}

describe('cooldown windows', () => {
  test.each<{ name: string; config: FlowConfig; params: Params; opened: FlowConfig }>([
    { name: 'legacy channel', config: { seconds: 30, perUser: false }, params: { global_s: 30 }, opened: { channelSeconds: 30, viewerSeconds: 0 } },
    { name: 'legacy viewer', config: { seconds: 120, perUser: true }, params: { user_s: 120 }, opened: { channelSeconds: 0, viewerSeconds: 120 } },
    { name: 'channel only', config: { channelSeconds: 30, viewerSeconds: 0 }, params: { global_s: 30 }, opened: { channelSeconds: 30, viewerSeconds: 0 } },
    { name: 'viewer only', config: { channelSeconds: 0, viewerSeconds: 120 }, params: { user_s: 120 }, opened: { channelSeconds: 0, viewerSeconds: 120 } },
    {
      name: 'both windows ignore the name', config: { channelSeconds: 30, viewerSeconds: 120, name: 'break' },
      params: { global_s: 30, user_s: 120 }, opened: { channelSeconds: 30, viewerSeconds: 120 }
    },
    {
      name: 'channel with a name', config: { channelSeconds: 30, viewerSeconds: 0, name: 'break' },
      params: { global_s: 30, name: 'break' }, opened: { channelSeconds: 30, viewerSeconds: 0, name: 'break' }
    },
    { name: 'no windows ignore the name', config: { channelSeconds: 0, viewerSeconds: 0, name: 'break' }, params: {}, opened: { channelSeconds: 0, viewerSeconds: 0 } },
    {
      name: 'time-left replies', config: { channelSeconds: 30, leftCommand: '!timeleft', leftReply: '{bits}', readyReply: 'Ready {bits}' },
      params: { global_s: 30, left_command: '!timeleft', left_reply: '{event.amount}', ready_reply: 'Ready {event.amount}' },
      opened: { channelSeconds: 30, viewerSeconds: 0, leftCommand: '!timeleft', leftReply: '{bits}', readyReply: 'Ready {bits}' }
    }
  ])('$name survives lowering and reopening', ({ config, params, opened }) => {
    const saved = lowered(cooldownFlow(node('t', 'twitch.cheer'), config));
    const board = rebuilt(saved);
    expect({ params: saved.conditions?.[0]?.params, reopened: board.nodes[1]?.config, resaved: lowered(board) })
      .toEqual({ params, reopened: opened, resaved: saved });
  });

  test.each(['leftReply', 'readyReply'])('a cooldown %s rejects an event token the trigger lacks', (field) => {
    const drawn = cooldownFlow(node('t', 'twitch.follow'), { channelSeconds: 30, leftCommand: '!timeleft', [field]: '{bits}' });
    expect(flowToDefinition(drawn)).toEqual({ ok: false, issues: [{ path: `nodes.g.${field}.bits`, code: 'unmappable_token' }] });
  });
});

describe('target picker round-trip', () => {
  const actions = [
    { type: 'timeout', nodeType: 'reply.timeout', params: { seconds: 600 } },
    { type: 'ban', nodeType: 'reply.ban', params: { reason: '{user} broke a rule' } },
    { type: 'unban', nodeType: 'reply.unban', params: {} },
    { type: 'mod', nodeType: 'reply.mod', params: { on: true }, fields: { change: 'add' } },
    { type: 'mod', nodeType: 'reply.mod', params: {} },
    { type: 'vip', nodeType: 'reply.vip', params: { on: false }, fields: { change: 'remove' } },
    { type: 'points_add', nodeType: 'reply.points', params: { amount: -10 } },
    { type: 'points_set', nodeType: 'reply.points_set', params: { amount: 0 } }
  ];
  const targets = [
    { mode: 'implicit login', wire: { login: '{touser}' }, config: { target: '{touser}' } },
    { mode: 'explicit login', wire: { who: 'login', login: 'bagel' }, config: { who: 'login', target: 'bagel' } },
    { mode: 'viewer', wire: { who: 'viewer' }, config: { who: 'viewer' } },
    { mode: 'mention', wire: { who: 'mention' }, config: { who: 'mention' } },
    { mode: 'set viewer', wire: { who: 'user', user_id: '1234567', login: 'bagel' }, config: { who: 'user', userId: '1234567', login: 'bagel' } }
  ];

  test.each(actions.flatMap((entry) => targets.map((target) => ({ ...entry, ...target }))))(
    '$type preserves $mode when reopened and resaved',
    ({ type, nodeType, params, fields = params, wire, config }) => {
      const definition: Definition = {
        version: 1,
        trigger: { type: 'command', params: { name: '!test' } },
        actions: [{ type, params: { ...params, ...wire } }]
      };
      const reopened = rebuilt(definition);
      const step = reopened.nodes.find((node) => node.type === nodeType);
      expect({ config: step?.config, definition: lowered(reopened) }).toEqual({ config: { ...fields, ...config }, definition });
    }
  );

  test('switching away from a set viewer leaves its draft selection out of the saved action', () => {
    const trigger = node('t', 'twitch.chat.command', { trigger: '!test' });
    const step = node('r', 'reply.points', { amount: 10, who: 'mention', userId: '1234567', login: 'bagel', target: '{bits}' });
    expect(lowered(flow([trigger, step], [edge('t', 'next', 'r')])).actions).toEqual([
      { type: 'points_add', params: { amount: 10, who: 'mention' } }
    ]);
  });
});

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

type LoweringChecks = Pick<Definition, 'conditions' | 'actions'> & {
  body?: Action[];
  secondAction?: Action;
  actionCount?: number;
  bodyCount?: number;
  joinEdges?: number;
};

type LoweringCase = {
  name: Label;
  nodes: FlowNode[];
  edges: FlowEdge[];
  expected: LoweringChecks;
  roundtrip?: 'definition' | 'shape';
  join?: Label;
};

const followTrigger = node('t', 'twitch.follow');
const roleGate = node('g', 'cond.role', { level: 'mod' });
const moderator = { type: 'perm', params: { level: 'mod' } };
const say = (id: FlowNode['id'], message = id) => node(id, 'reply.chat', { message });
const loop = (id: FlowNode['id'], times = '3') => node(id, 'reply.loop', { times });
const get = (id: FlowNode['id']) => node(id, 'reply.http_get', { url: `https://api.example.com/${id}`, saveAs: id });

const gateCases: LoweringCase[] = [
  {
    name: 'an empty role level lowers to everyone',
    nodes: [followTrigger, node('g', 'cond.role', { level: '' }), say('a', 'x')],
    edges: [edge('t', 'next', 'g'), edge('g', 'yes', 'a')],
    expected: { conditions: [{ type: 'perm', params: { level: 'everyone' } }] }
  },
  {
    name: 'a leading gate with no blocked edge becomes a top-level condition',
    nodes: [followTrigger, roleGate, say('a', 'x')],
    edges: [edge('t', 'next', 'g'), edge('g', 'yes', 'a')],
    expected: { conditions: [moderator], actions: [chat('x')] }
  },
  {
    name: 'a gate with a blocked edge becomes if',
    nodes: [followTrigger, roleGate, say('a', 'x'), say('b', 'y')],
    edges: [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', 'no', 'b')],
    expected: {
      conditions: undefined,
      actions: [{ type: 'if', params: { condition: moderator, then: [chat('x')], else: [chat('y')] } }]
    }
  },
  {
    name: 'a gate after an action is an if even with no blocked edge',
    nodes: [followTrigger, say('a', 'x'), roleGate, say('b', 'y')],
    edges: [edge('t', 'next', 'a'), edge('a', 'next', 'g'), edge('g', 'yes', 'b')],
    expected: { actions: [chat('x'), { type: 'if', params: { condition: moderator, then: [chat('y')] } }] }
  },
  {
    name: 'a clean merge lowers to if then the shared step and rebuilds as a join',
    nodes: [followTrigger, roleGate, say('a', 'yes'), say('b', 'no'), say('s', 'shared')],
    edges: [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', 'no', 'b'), edge('a', 'next', 's'), edge('b', 'next', 's')],
    expected: {
      actions: [{ type: 'if', params: { condition: moderator, then: [chat('yes')], else: [chat('no')] } }, chat('shared')],
      joinEdges: 2
    },
    roundtrip: 'definition',
    join: 'shared'
  },
  {
    name: 'a branch joining directly from the blocked port keeps an empty else',
    nodes: [followTrigger, roleGate, say('a', 'yes'), say('s', 'shared')],
    edges: [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', 'no', 's'), edge('a', 'next', 's')],
    expected: { actions: [{ type: 'if', params: { condition: moderator, then: [chat('yes')] } }, chat('shared')] },
    roundtrip: 'definition'
  }
];

const forkCases: LoweringCase[] = [
  {
    name: 'fan-out lowers to parallel paths in edge order',
    nodes: [followTrigger, say('b', 'second'), say('a', 'first')],
    edges: [edge('t', 'next', 'a'), edge('t', 'next', 'b')],
    expected: {
      actions: [{ type: 'parallel', params: { branches: [{ weight: 1, actions: [chat('first')] }, { weight: 1, actions: [chat('second')] }] } }]
    }
  },
  {
    name: 'a loop in a fork stays nested inside its path',
    nodes: [followTrigger, loop('l'), say('body'), say('other')],
    edges: [edge('t', 'next', 'l'), edge('t', 'next', 'other'), edge('l', 'body', 'body')],
    expected: {
      actions: [{ type: 'parallel', params: { branches: [
        { weight: 1, actions: [{ type: 'loop', params: { times: '3', actions: [chat('body')] } }] },
        { weight: 1, actions: [chat('other')] }
      ] } }]
    },
    roundtrip: 'shape'
  },
  {
    name: 'a fork in a loop stays inside the body',
    nodes: [followTrigger, loop('l'), say('a'), say('b'), say('after')],
    edges: [edge('t', 'next', 'l'), edge('l', 'body', 'a'), edge('l', 'body', 'b'), edge('l', 'done', 'after')],
    expected: {
      actions: [
        { type: 'loop', params: {
          times: '3',
          actions: [{ type: 'parallel', params: { branches: [{ weight: 1, actions: [chat('a')] }, { weight: 1, actions: [chat('b')] }] } }]
        } },
        chat('after')
      ]
    },
    roundtrip: 'shape'
  }
];

const apiPaths = { type: 'http_get', params: { url: 'https://api.example.com/g', save_as: 'g', on_ok: [chat('worked')], on_failure: [chat('failed')] } };

const apiCases: LoweringCase[] = [
  {
    name: 'a loop holding an API node keeps both ports inside the body',
    nodes: [followTrigger, loop('l'), get('g'), say('worked'), say('failed'), say('after')],
    edges: [edge('t', 'next', 'l'), edge('l', 'body', 'g'), edge('g', 'ok', 'worked'), edge('g', 'failure', 'failed'), edge('l', 'done', 'after')],
    expected: { body: [apiPaths], secondAction: chat('after') },
    roundtrip: 'shape'
  },
  {
    name: 'a Failed path leading to a join runs the join after the node, not inside a path',
    nodes: [followTrigger, get('g'), say('worked'), say('failed'), say('join')],
    edges: [edge('t', 'next', 'g'), edge('g', 'ok', 'worked'), edge('g', 'failure', 'failed'), edge('worked', 'next', 'join'), edge('failed', 'next', 'join')],
    expected: { actions: [apiPaths, chat('join')] },
    roundtrip: 'shape'
  },
  {
    name: 'a Failed port wired straight to the step after a Worked path is a join',
    nodes: [followTrigger, get('g'), say('worked'), say('join')],
    edges: [edge('t', 'next', 'g'), edge('g', 'ok', 'worked'), edge('worked', 'next', 'join'), edge('g', 'failure', 'join')],
    expected: {
      actions: [{ type: 'http_get', params: { url: 'https://api.example.com/g', save_as: 'g', on_ok: [chat('worked')] } }, chat('join')]
    },
    roundtrip: 'shape'
  },
  {
    name: 'two paths with no join at all end where they are drawn',
    nodes: [followTrigger, get('g'), say('worked'), say('failed')],
    edges: [edge('t', 'next', 'g'), edge('g', 'ok', 'worked'), edge('g', 'failure', 'failed')],
    expected: { actions: [apiPaths] },
    roundtrip: 'shape'
  }
];

const loopCases: LoweringCase[] = [
  {
    name: 'a loop inside a loop keeps each body and each next step',
    nodes: [followTrigger, loop('outer', '2'), loop('inner', '3'), say('deep'), say('afterinner'), say('afterouter')],
    edges: [edge('t', 'next', 'outer'), edge('outer', 'body', 'inner'), edge('inner', 'body', 'deep'), edge('inner', 'done', 'afterinner'), edge('outer', 'done', 'afterouter')],
    expected: {
      actions: [
        { type: 'loop', params: { times: '2', actions: [{ type: 'loop', params: { times: '3', actions: [chat('deep')] } }, chat('afterinner')] } },
        chat('afterouter')
      ]
    },
    roundtrip: 'shape'
  },
  {
    name: 'an if inside a loop joins inside the body, not after the loop',
    nodes: [followTrigger, loop('l'), node('c', 'cond.chance', { percent: 50 }), say('yes'), say('no'), say('join')],
    edges: [edge('t', 'next', 'l'), edge('l', 'body', 'c'), edge('c', 'yes', 'yes'), edge('c', 'no', 'no'), edge('yes', 'next', 'join'), edge('no', 'next', 'join')],
    expected: { bodyCount: 2, actionCount: 1 },
    roundtrip: 'shape'
  },
  {
    name: 'a pause in a loop body is allowed and the step after the loop stays after it',
    nodes: [followTrigger, loop('l'), node('w', 'reply.wait', { seconds: 5 }), say('after')],
    edges: [edge('t', 'next', 'l'), edge('l', 'body', 'w'), edge('l', 'done', 'after')],
    expected: { secondAction: chat('after') },
    roundtrip: 'shape'
  }
];

function shape(input: Flow): string[] {
  const labelOf = (n: FlowNode) => String(n.config.message ?? n.config.url ?? n.config.times ?? n.type);
  const byId = new Map(input.nodes.map((n) => [n.id, n]));
  return input.edges.map((e) => `${labelOf(byId.get(e.from)!)} ${e.port} ${labelOf(byId.get(e.to)!)}`).sort();
}

function loweringChecks(definition: Definition, reopened: Flow | undefined, join: LoweringCase['join']): LoweringChecks {
  const { actions, conditions } = definition;
  const body = actions?.[0]?.params?.actions as Action[] | undefined;
  const shared = reopened?.nodes.find((n) => n.config.message === join);
  return {
    actions,
    conditions,
    body,
    secondAction: actions?.[1],
    actionCount: actions?.length,
    bodyCount: body?.length,
    joinEdges: reopened?.edges.filter((e) => e.to === shared?.id).length
  };
}

function reopenLowered(definition: Definition, roundtrip: LoweringCase['roundtrip']): Flow | undefined {
  if (!roundtrip) return undefined;
  const payload = roundtrip === 'shape' ? JSON.parse(JSON.stringify(definition)) as Definition : definition;
  return rebuilt(payload);
}

test.each([...gateCases, ...forkCases, ...apiCases, ...loopCases])('$name', ({ nodes, edges, expected, roundtrip, join }) => {
  const drawn = flow(nodes, edges);
  const definition = lowered(drawn);
  const preserveShape = roundtrip === 'shape';
  const reopened = reopenLowered(definition, roundtrip);
  const checks = loweringChecks(definition, reopened, join);
  const keys = Object.keys(expected) as (keyof LoweringChecks)[];
  expect({
    lowering: Object.fromEntries(keys.map((key) => [key, checks[key]])),
    roundtrip: reopened ? lowered(reopened) : undefined,
    shape: preserveShape ? shape(reopened!) : undefined,
    issues: preserveShape ? validateDefinition(definition) : undefined
  }).toEqual({
    lowering: expected,
    roundtrip: roundtrip ? definition : undefined,
    shape: preserveShape ? shape(drawn) : undefined,
    issues: preserveShape ? [] : undefined
  });
});

type TriggerCase = {
  name: string;
  type: string;
  config: FlowConfig;
  trigger: Trigger;
  raised?: { type: string; config: FlowConfig };
};

const commandSettings = [false, true].flatMap((caseSensitive) => [false, true].map((subMessages) => ({
  draft: { caseSensitive, subMessages },
  wire: { ...(caseSensitive ? { case_sensitive: true } : {}), ...(subMessages ? { sources: ['chat', 'sub_message'] } : {}) }
})));

function commandCases(): TriggerCase[] {
  return commandSettings.flatMap(({ draft, wire }) => ['start', 'exact'].flatMap((match) =>
    [false, true].map((hasAliases) => ({
      name: `command ${match} ${JSON.stringify(draft)} aliases=${hasAliases}`,
      type: 'twitch.chat.command',
      config: { trigger: ' Hug ', match, ...draft, ...(hasAliases ? { aliases: ' cuddle, , !squeeze, ' } : {}) },
      trigger: { type: 'command', params: {
        name: '!Hug', ...(match === 'exact' ? { match } : {}), ...wire, ...(hasAliases ? { aliases: ['!cuddle', '!squeeze'] } : {})
      } },
      raised: { type: 'twitch.chat.command', config: {
        trigger: '!Hug', match, ...draft, ...(hasAliases ? { aliases: '!cuddle, !squeeze' } : {})
      } }
    }))
  ));
}

const wordModes = [
  { match: 'word', word: 'a.b', mode: 'regex', name: '(?:^|[^\\p{L}\\p{N}_])a\\.b(?:$|[^\\p{L}\\p{N}_])' },
  { match: 'anywhere', word: 'good morning', mode: 'anywhere', name: 'good morning' },
  { match: 'pattern', word: '^Bagel[!?]$', mode: 'regex', name: '^Bagel[!?]$' }
];

const wordCases: TriggerCase[] = commandSettings.flatMap(({ draft, wire }) => wordModes.map(({ match, word, mode, name }) => ({
  name: `word ${match} ${JSON.stringify(draft)}`,
  type: 'twitch.chat.word',
  config: { word, match, ...draft },
  trigger: { type: 'command', params: { name, match: mode, ...wire } }
})));

const subscriptionKinds = [
  { variant: 'any', kind: 'channel.subscribe', filters: {}, months: {} },
  { variant: 'new', kind: 'channel.subscribe', filters: { is_gift: 'false' }, months: {} },
  { variant: 'gifted', kind: 'channel.subscribe', filters: { is_gift: 'true' }, months: {} },
  { variant: 'resub', kind: 'channel.subscription.message', filters: { months: '1', streak: '12' }, months: { minMonths: 1, minStreak: 12 } }
];

function subscriptionCases(): TriggerCase[] {
  return subscriptionKinds.flatMap(({ variant, kind, filters, months }) => ['any', '1000', '2000', '3000'].map((tier) => {
    const tierFilter = tier === 'any' ? {} : { tier };
    const eventFilters = { ...filters, ...tierFilter };
    const params = Object.keys(eventFilters).length ? { kind, filters: eventFilters } : { kind };
    return {
      name: `subscription ${variant} tier=${tier}`,
      type: 'twitch.sub',
      config: { variant, tier, minMonths: 1, minStreak: 12 },
      trigger: { type: 'event', params },
      raised: { type: 'twitch.sub', config: { variant, tier, ...months } }
    };
  }));
}

const actors = [
  { option: 'anyone', filters: {} },
  { option: 'named', filters: { anonymous: 'false' } },
  { option: 'anonymous', filters: { anonymous: 'true' } }
];

const actorEvents = [
  { type: 'twitch.sub_gift', kind: 'channel.subscription.gift', actor: 'gifter', config: { minGifts: 1, tier: '3000' }, filters: { amount: '1', tier: '3000' } },
  { type: 'twitch.cheer', kind: 'channel.cheer', actor: 'cheerer', config: { minBits: 1 }, filters: { amount: '1' } }
];

const actorCases: TriggerCase[] = actorEvents.flatMap((event) => actors.map(({ option, filters }) => ({
  name: `${event.type} ${option}`,
  type: event.type,
  config: { ...event.config, [event.actor]: option },
  trigger: { type: 'event', params: { kind: event.kind, filters: { ...event.filters, ...filters } } }
})));

const importedCommandCases: TriggerCase[] = [
  {
    name: 'imported exact command with a bare name and sub-message-only source',
    type: 'twitch.chat.command',
    config: { trigger: 'Hug', importedName: 'Hug', match: 'exact', caseSensitive: false, aliases: 'cuddle, !squeeze', importedAliases: 'cuddle\n!squeeze',
      subMessages: true, importedSources: 'sub_message' },
    trigger: { type: 'command', params: { name: 'Hug', match: 'exact', aliases: ['cuddle', '!squeeze'], sources: ['sub_message'] } }
  },
  {
    name: 'imported substring with a comma inside an alias',
    type: 'twitch.chat.word',
    config: { word: 'bagel', match: 'anywhere', caseSensitive: false, aliases: 'bagel, toasted, lox', importedAliases: 'bagel, toasted\nlox',
      subMessages: true, importedSources: 'sub_message,chat' },
    trigger: { type: 'command', params: { name: 'bagel', match: 'anywhere', aliases: ['bagel, toasted', 'lox'], sources: ['sub_message', 'chat'] } }
  },
  {
    name: 'imported nonliteral word-like pattern',
    type: 'twitch.chat.word',
    config: { word: '(?:^|[^\\p{L}\\p{N}_])a.b(?:$|[^\\p{L}\\p{N}_])', match: 'pattern', caseSensitive: false, subMessages: false },
    trigger: { type: 'command', params: { name: '(?:^|[^\\p{L}\\p{N}_])a.b(?:$|[^\\p{L}\\p{N}_])', match: 'regex' } }
  }
];

const thresholdCases: TriggerCase[] = [
  {
    name: 'resub without minimums',
    type: 'twitch.sub',
    config: { variant: 'resub', tier: 'any' },
    trigger: { type: 'event', params: { kind: 'channel.subscription.message' } }
  },
  {
    name: 'resub with only a streak of one',
    type: 'twitch.sub',
    config: { variant: 'resub', tier: 'any', minStreak: 1 },
    trigger: { type: 'event', params: { kind: 'channel.subscription.message', filters: { streak: '1' } } }
  },
  {
    name: 'resub with only a month threshold',
    type: 'twitch.sub',
    config: { variant: 'resub', tier: 'any', minMonths: 12 },
    trigger: { type: 'event', params: { kind: 'channel.subscription.message', filters: { months: '12' } } }
  },
  {
    name: 'legacy gift subscription',
    type: 'twitch.sub',
    config: { variant: 'gift', minGifts: 5, tier: '2000', gifter: 'anonymous', minMonths: 3 },
    trigger: { type: 'event', params: { kind: 'channel.subscription.gift', filters: { amount: '5', tier: '2000', anonymous: 'true' } } },
    raised: { type: 'twitch.sub_gift', config: { minGifts: 5, tier: '2000', gifter: 'anonymous' } }
  },
  {
    name: 'one raider',
    type: 'twitch.raid',
    config: { minViewers: 1 },
    trigger: { type: 'event', params: { kind: 'channel.raid', filters: { viewers: '1' } } }
  },
  {
    name: 'fractional cheer minimum is omitted',
    type: 'twitch.cheer',
    config: { minBits: 1.5 },
    trigger: { type: 'event', params: { kind: 'channel.cheer' } },
    raised: { type: 'twitch.cheer', config: { cheerer: 'anyone' } }
  }
];

const newTriggerCases: TriggerCase[] = [
  {
    name: 'timer with offline runs and jitter',
    type: 'schedule.timer',
    config: { every: 900, jitter: 60, chatLines: 5, offline: true },
    trigger: { type: 'timer', params: { interval_s: 900, random_jitter_s: 60, min_chat_lines: 5, offline_ok: true } }
  },
  {
    name: 'timer with an explicit zero chat minimum',
    type: 'schedule.timer',
    config: { every: 60, jitter: 0, chatLines: 0, offline: false },
    trigger: { type: 'timer', params: { interval_s: 60, min_chat_lines: 0 } }
  },
  {
    name: 'manual recipe',
    type: 'schedule.manual',
    config: {},
    trigger: { type: 'manual' }
  },
  {
    name: 'any ad break',
    type: 'twitch.ad_break',
    config: { startedBy: 'any' },
    trigger: { type: 'event', params: { kind: 'channel.ad_break.begin' } }
  },
  {
    name: 'automatic ad break with minimum length',
    type: 'twitch.ad_break',
    config: { startedBy: 'automatic', minLength: 90 },
    trigger: { type: 'event', params: { kind: 'channel.ad_break.begin', filters: { automatic: 'true', seconds: '90' } } }
  },
  {
    name: 'manual ad break',
    type: 'twitch.ad_break',
    config: { startedBy: 'manual' },
    trigger: { type: 'event', params: { kind: 'channel.ad_break.begin', filters: { automatic: 'false' } } }
  },
  {
    name: 'channel update in any category',
    type: 'twitch.channel_update',
    config: {},
    trigger: { type: 'event', params: { kind: 'channel.update' } }
  },
  {
    name: 'channel update filtered by category',
    type: 'twitch.channel_update',
    config: { category: 'Just Chatting' },
    trigger: { type: 'event', params: { kind: 'channel.update', filters: { category: 'Just Chatting' } } }
  }
];

test.each([
  ...commandCases(),
  ...wordCases,
  ...subscriptionCases(),
  ...actorCases,
  ...importedCommandCases,
  ...thresholdCases,
  ...newTriggerCases
])('trigger round trip: $name', ({ type, config, trigger, raised }) => {
  const definition = { version: 1, trigger, actions: [chat('Hello')] };
  const authored = flow([node('t', type, config), node('a', 'reply.chat', { message: 'Hello' })], [edge('t', 'next', 'a')]);
  const reopened = rebuilt(definition);
  const root = reopened.nodes[0]!;
  expect({
    authored: lowered(authored), saved: lowered(reopened),
    raised: { type: root.type, config: root.config }, issues: validateDefinition(definition)
  }).toEqual({ authored: definition, saved: definition, raised: raised ?? { type, config }, issues: [] });
});

test('an existing timer without a stored chat minimum opens and saves as zero', () => {
  const flow = rebuilt({ version: 1, trigger: { type: 'timer', params: { interval_s: 300 } }, actions: [chat('Hello')] });
  expect({ config: flow.nodes[0]!.config, trigger: lowered(flow).trigger }).toEqual({
    config: { every: 300, jitter: 0, chatLines: 0, offline: false },
    trigger: { type: 'timer', params: { interval_s: 300, min_chat_lines: 0 } }
  });
});

test.each<Trigger>([
  { type: 'command', params: { name: '!hug', future: true } },
  { type: 'event', params: { kind: 'channel.subscribe', filters: { months: '1' } } },
  { type: 'event', params: { kind: 'channel.subscription.gift', filters: { total: '12' } } },
  { type: 'event', params: { kind: 'channel.update', filters: { category_id: '10' } } }
])('unsupported trigger with extra filters or parameters: $params', (trigger) => {
  expect(definitionToFlow({ version: 1, trigger }, meta))
    .toEqual({ ok: false, issues: [{ path: 'trigger', code: 'unsupported_trigger' }] });
});

describe('reply tokens', () => {
  const line = (message: Label) => node('a', 'reply.chat', { message });
  const after = (trigger: FlowNode, message: Label) => flow([trigger, line(message)], [edge(trigger.id, 'next', 'a')]);
  const text = (definition: Definition) => definition.actions?.[0]?.params?.text;

  test.each([
    ['twitch.cheer', {}, '{bits} from {user}: {message}', '{event.amount} from {user}: {event.message}'],
    ['twitch.raid', {}, '{viewers} with {user.id}', '{event.viewers} with {user.id}'],
    ['twitch.sub', { variant: 'any' }, 'tier {tier}', 'tier {event.tier}'],
    ['twitch.sub', { variant: 'resub' }, '{months} at {tier} ({streak}): {message}', '{event.months} at {event.tier} ({event.streak}): {event.message}'],
    ['twitch.sub_gift', {}, '{count} at {tier} ({total})', '{event.amount} at {event.tier} ({event.total})'],
    ['twitch.reward', { reward: 'r' }, '{reward} for {cost}: {input}', '{event.reward} for {event.cost}: {event.input}'],
    ['twitch.unban_request', {}, '{text} ({event.id})', '{event.text} ({event.id})'],
    ['twitch.ad_break', {}, '{seconds}', '{event.seconds}'],
    ['twitch.channel_update', {}, '{title} in {game}', '{event.title} in {event.category}'],
    ['twitch.stream.online', {}, '{title} in {game}', '{title} in {game}'],
    ['twitch.chat.command', { trigger: '!hug' }, '{user} hugs {touser} {args}', '{user} hugs {touser} {args}'],
    ['twitch.chat.word', { word: 'bagel' }, '{args}', '{args}'],
    ['schedule.manual', {}, '{args}', '{args}']
  ] as [string, FlowConfig, string, string][])('%s %j writes %s as %s and reads it back', (type, config, drawn, stored) => {
    const definition = lowered(after(node('t', type, config), drawn));
    const again = rebuilt(definition);
    expect({ stored: text(definition), drawn: again.nodes[1]!.config.message, resaved: lowered(again) })
      .toEqual({ stored, drawn, resaved: definition });
  });

  test.each([
    ['twitch.sub', { variant: 'any' }, '{months}'],
    ['twitch.sub', { variant: 'new' }, '{months}'],
    ['twitch.sub', { variant: 'gift' }, '{months}'],
    ['twitch.follow', {}, '{bits}'],
    ['twitch.follow', {}, '{count}'],
    ['twitch.raid', {}, '{count}'],
    ['twitch.chat.command', { trigger: '!hug' }, '{message}'],
    ['twitch.sub', { variant: 'new' }, '{streak}'],
    ['twitch.sub', { variant: 'resub' }, '{total}'],
    ['twitch.follow', {}, '{reward}'],
    ['twitch.follow', {}, '{cost}'],
    ['twitch.follow', {}, '{text}'],
    ['twitch.follow', {}, '{seconds}']
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
    const definition = lowered(after(node('t', 'twitch.follow'), '{uptime} {event.message}'));
    expect([text(definition), rebuilt(definition).nodes[1]!.config.message])
      .toEqual(['{uptime} {event.message}', '{uptime} {event.message}']);
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

  test.each([
    { port: 'No', issues: [{ path: 'nodes.g', code: 'unknown_port' }] },
    { port: 'no', issues: [] }
  ])('validates the $port port on a chance check', ({ port, issues }) => {
    const nodes = [node('t', 'twitch.follow'), node('g', 'cond.chance', { percent: 50 }), chatNode('a'), chatNode('b')];
    const edges = [edge('t', 'next', 'g'), edge('g', 'yes', 'a'), edge('g', port, 'b')];
    const result = flowToDefinition(flow(nodes, edges));
    expect(result.ok ? validateDefinition(result.value) : result.issues).toEqual(issues);
  });

  test.each([
    { type: 'twitch.follow', port: 'yes' },
    { type: 'reply.chat', port: 'failure' },
    { type: 'reply.loop', port: 'next' },
    { type: 'reply.http_get', port: 'no' }
  ])('$type rejects the $port output port', ({ type, port }) => {
    const source = node('s', type);
    const target = chatNode('a');
    const input = type === 'twitch.follow'
      ? flow([source, target], [edge('s', port, 'a')])
      : flow([node('t', 'twitch.follow'), source, target], [edge('t', 'next', 's'), edge('s', port, 'a')]);
    expect(flowToDefinition(input)).toEqual({ ok: false, issues: [{ path: 'nodes.s', code: 'unknown_port' }] });
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

  test.each(['bagel', 'café', 'hi!', 'good morning', 'a.b*c', '(x)', '[y]', '{z}', '^$|', 'c\\1'])('%s validates and reopens as a whole word', (word) => {
    const definition = lowered(wordFlow(word));
    const raised = rebuilt(definition).nodes[0]!;
    expect({ match: definition.trigger.params!.match, issues: validateDefinition(definition), type: raised.type, config: raised.config })
      .toEqual({ match: 'regex', issues: [], type: 'twitch.chat.word', config: { word, match: 'word', caseSensitive: false, subMessages: false } });
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
});

describe('viewer list and check round-trips', () => {
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

  test.each<{ type: FlowNode['type']; condition: Condition; config?: FlowConfig; opened: FlowConfig; unresolved?: true }>([
    { type: 'cond.user', condition: users, config: gate.config, opened: { mode: 'only', users: '123456:' } },
    { type: 'cond.user', condition: { type: 'users', params: { allow_ids: ['1', '2', '3'] } }, config: { users: '1:alice,2:bob,3:carol' }, opened: { mode: 'only', users: '1:,2:,3:' } },
    { type: 'cond.user', condition: { type: 'users', params: { deny_ids: ['2', '3'] } }, opened: { mode: 'except', users: '2:,3:' } },
    { type: 'cond.user', condition: { type: 'users', params: { allow: [SETUP_LOGIN] } }, opened: { mode: 'only', login: SETUP_LOGIN } },
    { type: 'cond.user', condition: { type: 'users', params: { allow: ['alice', 'bob'], allow_ids: ['3'] } }, opened: { mode: 'only', users: '3:,:alice,:bob' }, unresolved: true },
    { type: 'cond.user', condition: { type: 'users', params: { deny: ['alice'] } }, opened: { mode: 'except', users: ':alice' }, unresolved: true },
    { type: 'cond.live', condition: { type: 'live', params: { state: 'offline' } }, opened: { state: 'offline' } },
    { type: 'cond.category', condition: { type: 'category', params: { allow: ['Just Chatting', 'Minecraft'] } }, config: { games: ' Just Chatting, , Minecraft ' }, opened: { mode: 'allow', games: 'Just Chatting, Minecraft' } },
    { type: 'cond.category', condition: { type: 'category', params: { deny: ['Minecraft'] } }, opened: { mode: 'deny', games: 'Minecraft' } },
    { type: 'cond.pattern', condition: { type: 'arg_regex', params: { pattern: '^[0-9]+$' } }, opened: { pattern: '^[0-9]+$' } },
    { type: 'cond.follow', condition: { type: 'follow', params: { min_days: 0 } }, opened: { minDays: 0 } },
    { type: 'cond.follow', condition: { type: 'follow', params: { min_days: 30 } }, opened: { minDays: 30 } },
    { type: 'cond.accountAge', condition: { type: 'account_age', params: { min_days: 7 } }, opened: { minDays: 7 } }
  ])('$type opens $condition.params', ({ type, condition, config, opened, unresolved }) => {
    const start = node('t', 'twitch.chat.command', { trigger: '!check' });
    const board = flow([start, node('u', type, config ?? opened), yes], [edge('t', 'next', 'u'), edge('u', 'yes', 'y')]);
    const definition = { ...lowered(flow([start, yes], [edge('t', 'next', 'y')])), conditions: [condition] };
    const restored = rebuilt(definition).nodes[1]!;
    expect({ type: restored.type, config: restored.config, saved: flowToDefinition(board) }).toEqual({
      type, config: opened, saved: unresolved ? { ok: false, issues: [{ path: 'nodes.u', code: 'user_not_resolved' }] } : { ok: true, value: definition }
    });
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
    ['allow and deny IDs', { allow_ids: ['1'], deny_ids: ['2'] }],
    ['allow IDs and deny logins', { allow_ids: ['1'], deny: ['b'] }]
  ])('%s is not drawn as one viewer list', (_label, params) => {
    const result = definitionToFlow({ version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, conditions: [{ type: 'users', params }], actions: [chat('a')] }, meta);
    expect(result).toEqual({ ok: false, issues: [{ path: 'conditions[0]', code: 'unsupported_condition' }] });
  });
});

describe('set lights step', () => {
  const trigger = node('t', 'twitch.follow');
  const withStep = (step: FlowNode) => flow([trigger, step], [edge('t', 'next', step.id)]);
  const target = { device: 'AA:BB', sku: 'H6008', deviceName: 'Desk' };
  const wireTarget = { device: target.device, sku: target.sku, device_name: target.deviceName };
  const color = { mode: 'color', color: '#f5a524' };
  const scene = {
    mode: 'scene',
    sceneName: 'Sunset',
    sceneType: 'devices.capabilities.dynamic_scene',
    sceneInstance: 'snapshot',
    sceneValue: '{"paramId":4,"id":9}'
  };
  const scenePayload = {
    mode: 'scene',
    scene_type: scene.sceneType,
    scene_instance: scene.sceneInstance,
    scene_value: scene.sceneValue,
    scene_name: scene.sceneName
  };
  const numericScenePayload = { ...scenePayload, scene_instance: 'lightScene', scene_value: '0' };

  type LightCase = { name: Label; config: FlowConfig; params: Params; draft?: FlowConfig; savedParams?: Params };
  const cases: LightCase[] = [
    { name: 'legacy color without new keys', config: color, params: color },
    { name: 'default color', config: { ...color, color: '#ffffff' }, params: { ...color, color: '#ffffff' }, draft: { color: '', brightness: '' } },
    { name: 'color with extra lights', config: { ...color, extraLights: 'CC:DD/H6159,EE:FF/H6008' }, params: { ...color, extra_lights: ['CC:DD/H6159', 'EE:FF/H6008'] } },
    { name: 'color any time', config: { ...color, liveOnly: false }, params: { ...color, any_time: true } },
    { name: 'scene', config: { ...scene, scene: 'snapshot:4:9' }, params: { ...scenePayload, scene_id: 'snapshot:4:9' } },
    {
      name: 'scene with a saved pair and no ID', config: { ...scene, scene: 'snapshot:4:9' },
      params: { ...scenePayload, scene_id: 'snapshot:4:9' }, savedParams: scenePayload
    },
    {
      name: 'scene with a saved number and no ID', config: { ...scene, scene: 'lightScene:0', sceneInstance: 'lightScene', sceneValue: '0' },
      params: { ...numericScenePayload, scene_id: 'lightScene:0' }, savedParams: numericScenePayload
    },
    { name: 'default white', config: { mode: 'white', kelvin: 4000 }, params: { mode: 'white', kelvin: 4000 }, draft: { kelvin: '', brightness: 0 } },
    { name: 'white with brightness and hold', config: { mode: 'white', kelvin: 5200, brightness: 72, holdS: 5 }, params: { mode: 'white', kelvin: 5200, brightness: 72, hold_s: 5 }, draft: { kelvin: '5200', brightness: '72', holdS: '5' } },
    { name: 'on with brightness', config: { mode: 'on', brightness: 40 }, params: { mode: 'on', brightness: 40 } },
    { name: 'off discards the draft brightness and color', config: { mode: 'off' }, params: { mode: 'off' }, draft: { brightness: 70, color: '#f5a524', holdS: 0 } }
  ];

  test.each(cases)('$name lowers and reopens', ({ config, params, draft, savedParams }) => {
    const definition = lowered(withStep(node('l', 'reply.lights', { ...target, ...config, ...draft })));
    const actions = [{ type: 'lights', params: { ...wireTarget, ...params } }];
    const saved = { ...definition, actions: [{ type: 'lights', params: { ...wireTarget, ...(savedParams ?? params) } }] };
    const reopened = rebuilt(saved);
    expect({ actions: definition.actions, issues: validateDefinition(definition), config: reopened.nodes[1]?.config, resaved: lowered(reopened).actions }).toEqual({
      actions, issues: [], config: { ...target, liveOnly: true, ...config }, resaved: actions
    });
  });

  test('an unknown mode is validated on mode', () => {
    const step = node('l', 'reply.lights', { ...target, mode: 'blink', color: 'invalid' });
    const definition = lowered(withStep(step));
    expect({ params: definition.actions?.[0]?.params, issues: validateDefinition(definition), reopened: definitionToFlow(definition, meta) }).toEqual({
      params: { ...wireTarget, mode: 'blink' },
      issues: [{ path: 'actions[0].params.mode', code: 'invalid_enum' }],
      reopened: { ok: false, issues: [{ path: 'actions[0]', code: 'unsupported_action' }] }
    });
  });

  test('out-of-range brightness is validated', () => {
    const step = node('l', 'reply.lights', { ...target, mode: 'on', brightness: 101 });
    expect(validateDefinition(lowered(withStep(step))).map((issue) => issue.path)).toEqual(['actions[0].params.brightness']);
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
      conditions: [{ type: 'compare', params: { left: '{user}', op: 'unknown', right: 'a' } }],
      actions: [chat('a')]
    };
    expect(definitionToFlow(stored, meta)).toEqual({ ok: false, issues: [{ path: 'conditions[0]', code: 'unsupported_condition' }] });
  });

  test.each([
    { flag: {}, emitted: {}, matchCase: true },
    { flag: { ignore_case: false }, emitted: {}, matchCase: true },
    { flag: { ignore_case: true }, emitted: { ignore_case: true }, matchCase: false }
  ])('a saved comparison round-trips case matching: $flag', ({ flag, emitted, matchCase }) => {
    const params = { left: 'Bagel', op: 'starts', right: 'bagel' };
    const stored: Definition = {
      version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } },
      conditions: [{ type: 'compare', params: { ...params, ...flag } }], actions: [chat('a')]
    };
    const restored = rebuilt(stored);
    expect({
      matchCase: restored.nodes[1]?.config.matchCase,
      conditions: lowered(restored).conditions
    }).toEqual({ matchCase, conditions: [{ type: 'compare', params: { ...params, ...emitted } }] });
  });
});

describe('reading stored definitions', () => {
  const disabledControls: Record<string, Action> = {
    if: { type: 'if', enabled: false, params: { condition: { type: 'chance', params: { percent: 50 } }, then: [chat('yes')], else: [chat('no')] } },
    parallel: { type: 'parallel', enabled: false, params: { branches: [{ actions: [chat('a')] }, { actions: [chat('b')] }] } }
  };

  const placements: { name: string; wrap: (action: Action) => Action; path: string }[] = [
    { name: 'root', wrap: (action) => action, path: 'actions[0]' },
    { name: 'loop', wrap: (action) => ({ type: 'loop', params: { times: '2', actions: [action] } }), path: 'actions[0].params.actions[0]' },
    { name: 'parallel', wrap: (action) => ({ type: 'parallel', params: { branches: [{ actions: [action] }] } }), path: 'actions[0].params.branches[0].actions[0]' },
    { name: 'if', wrap: (action) => ({ type: 'if', params: { condition: { type: 'chance', params: { percent: 50 } }, then: [action] } }), path: 'actions[0].then[0]' }
  ];

  test.each(Object.entries(disabledControls).flatMap(([type, action]) => placements.map((placement) => ({ type, action, ...placement }))))(
    'refuses disabled $type under $name without enabling its contents',
    ({ action, wrap, path }) => {
      const definition: Definition = { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, actions: [wrap(action)] };
      expect(definitionToFlow(definition, meta)).toEqual({ ok: false, issues: [{ path, code: 'unsupported_disabled_control_flow' }] });
    }
  );

  test('a stored removed post, an action that no longer exists, cannot be drawn', () => {
    const stored: Definition = {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.raid' } },
      actions: [chat('a'), { type: 'removed_post', params: { text: 'hi' } }]
    };
    expect(definitionToFlow(stored, meta)).toEqual({ ok: false, issues: [{ path: 'actions[1]', code: 'unsupported_action' }] });
    expect(validateDefinition(stored)).toEqual([{ path: 'actions[1].type', code: 'unknown_type' }]);
  });

  test('a definition the builder cannot draw reports where', () => {
    const result = definitionToFlow(
      { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow', filters: { login: 'lotti' } } }, actions: [] },
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
});

describe('containers keep their drawn shape through save and reload', () => {
  const trigger = () => node('t', 'twitch.follow');

  function expectStable(drawn: Flow): Definition {
    const saved = lowered(drawn);
    expect(validateDefinition(saved)).toEqual([]);
    const payload = JSON.parse(JSON.stringify(saved)) as Definition;
    const reopened = rebuilt(payload);
    expect(shape(reopened)).toEqual(shape(drawn));
    expect(lowered(reopened)).toEqual(saved);
    return saved;
  }

  test('a step after an API node with no paths takes both ports and saves as the next step', () => {
    const old: Definition = { version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } }, actions: [{ type: 'http_post', params: { url: 'https://hooks.example.org/a', body: 'x' } }, chat('next')] };
    const reopened = rebuilt(old);
    expect(reopened.edges.filter((e) => e.port === 'ok' || e.port === 'failure').length).toBe(2);
    expect(lowered(reopened)).toEqual(old);
  });

  test('a fork after two API paths stays one fork after the node', () => {
    const fork = { type: 'parallel', params: { branches: [{ weight: 1, actions: [chat('c')] }, { weight: 1, actions: [chat('d')] }] } };
    const definition: Definition = {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.follow' } },
      actions: [{ type: 'http_get', params: { url: 'https://api.example.com/g', on_ok: [chat('a')] } }, fork]
    };
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test('a pause in a looping fork path before a join is refused like any pausing fork', () => {
    const drawn = flow(
      [trigger(), loop('l'), node('w', 'reply.wait', { seconds: 5 }), say('a'), say('join')],
      [edge('t', 'next', 'l'), edge('t', 'next', 'a'), edge('l', 'body', 'w'), edge('l', 'done', 'join'), edge('a', 'next', 'join')]
    );
    expect(issueCodes(drawn)).toEqual(['unstructured_merge']);
  });

  test('every new step lowers to its contract shape and validates', () => {
    const steps: [FlowNode, unknown][] = [
      [node('s', 'reply.wait', { seconds: 0.5 }), { type: 'delay', params: { ms: 500 } }],
      [node('s', 'reply.wait', { seconds: 1.001 }), { type: 'delay', params: { ms: 1001 } }],
      [node('s', 'reply.math', { expression: '1+1', saveAs: 'sum' }), { type: 'math', params: { expression: '1+1', save_as: 'sum' } }],
      [node('s', 'reply.reward_update', { reward: 'r1', setEnabled: 'off', reset: true }), { type: 'reward_update', params: { reward_id: 'r1', enabled: false, cost: null, reset_on_stream_end: true } }]
    ];
    for (const [step, want] of steps) {
      const saved = expectStable(flow([trigger(), step], [edge('t', 'next', 's')]));
      expect(saved.actions).toEqual([want as NonNullable<Definition['actions']>[number]]);
    }
  });

  test.each([
    { change: 'set', type: 'var_set', operand: { value: '{user}' }, config: { value: '{user}', amount: 1 } },
    { change: 'add', type: 'var_add', operand: { amount: 2 }, config: { amount: 2 } },
    { change: 'subtract', type: 'var_add', operand: { amount: -2 }, config: { amount: 2 } },
    { change: 'forget', type: 'var_delete', operand: {}, config: { amount: 1 } }
  ])('saved variable action opens as $change and round-trips', ({ change, type, operand, config }) => {
    const identity = { scope: 'viewer', name: 'round2' };
    const definition: Definition = {
      version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } },
      actions: [{ type, params: { ...identity, ...operand } }]
    };
    const reopened = rebuilt(definition);
    const step = reopened.nodes[1];
    expect({ type: step.type, config: step.config, actions: lowered(reopened).actions, issues: validateDefinition(definition) })
      .toEqual({ type: 'reply.var', config: { ...identity, do: change, ...config }, actions: definition.actions, issues: [] });
  });

  test.each([
    ['upper', '', ''], ['lower', '', ''], ['trim', '', ''], ['length', '', ''],
    ['replace', 'a', '2'], ['substring', '1', '2'], ['split_pick', ',', '2'],
    ['contains', 'a', ''], ['url_encode', '', ''], ['count', ',', '']
  ])('saved %s arguments reopen as generic operands and lower only when used', (op, first, second) => {
    const arg1 = first || 'unused';
    const arg2 = second || 'unused';
    const definition: Definition = {
      version: 1, trigger: { type: 'event', params: { kind: 'channel.follow' } },
      actions: [{ type: 'text', params: { op, input: '{user}', arg1, arg2, save_as: 'out' } }]
    };
    const reopened = rebuilt(definition);
    const step = reopened.nodes[1];
    expect({ type: step.type, config: step.config, actions: lowered(reopened).actions }).toEqual({
      type: 'reply.text', config: { op, input: '{user}', arg1, arg2, saveAs: 'out' },
      actions: [{ type: 'text', params: { op, input: '{user}', arg1: first, arg2: second, save_as: 'out' } }]
    });
  });

  test.each([
    { path: 'a.0.b', segments: ['a', '0', 'b'] },
    { path: ' a[0].b ', segments: ['a', '0', 'b'] },
    { path: 'a["key-1"][2][\'b\']', segments: ['a', 'key-1', '2', 'b'] },
    { path: '[0].name', segments: ['0', 'name'] },
    { path: '   ', segments: undefined },
    { path: 'a[0]b', segments: ['a[0]b'] }
  ])('web read lowers JSON path "$path"', ({ path, segments }) => {
    const step = node('g', 'reply.http_get', { url: 'https://example.com/data', jsonPath: path });
    expect(lowered(flow([trigger(), step], [edge('t', 'next', 'g')])).actions?.[0]?.params?.json_path).toEqual(segments);
  });

  test.each([
    { op: 'exists', value: '' },
    { op: 'exists', value: '{bits}' },
    { op: 'not_exists', value: '{bits}' }
  ])('a var $op gate ignores its inactive value $value and reopens', ({ op, value }) => {
    const saved = expectStable(
      flow([trigger(), node('v', 'cond.var', { scope: 'channel', name: 'mode', op, value }), say('a')], [edge('t', 'next', 'v'), edge('v', 'yes', 'a')])
    );
    expect(saved.conditions).toEqual([{ type: 'var', params: { scope: 'channel', name: 'mode', op } }]);
  });

  test.each([
    { op: 'upper', field: 'arg1', issues: [], args: ['', ''] },
    { op: 'contains', field: 'arg2', issues: [], args: ['', ''] },
    { op: 'replace', field: 'arg1', issues: [{ path: 'nodes.s.arg1.bits', code: 'unmappable_token' }], args: undefined },
    { op: 'split_pick', field: 'arg2', issues: [{ path: 'nodes.s.arg2.bits', code: 'unmappable_token' }], args: undefined }
  ])('a Follow trigger token-checks $field only if $op uses it', ({ op, field, issues, args }) => {
    const step = node('s', 'reply.text', { op, input: '{user}', [field]: '{bits}', saveAs: 'out' });
    const result = flowToDefinition(flow([trigger(), step], [edge('t', 'next', 's')]));
    const outcome = result.ok
      ? { issues: [], args: [result.value.actions?.[0]?.params?.arg1, result.value.actions?.[0]?.params?.arg2] }
      : { issues: result.issues, args: undefined };
    expect(outcome).toEqual({ issues, args });
  });

  test('builder tokens inside new templates lower to the event and raise back', () => {
    const drawn = flow(
      [node('t', 'twitch.cheer', { minBits: 1 }), node('m', 'reply.math', { expression: '{bits}*2', saveAs: 'double' }), node('l', 'reply.loop', { times: '{bits}' })],
      [edge('t', 'next', 'm'), edge('m', 'next', 'l')]
    );
    const saved = lowered(drawn);
    expect(saved.actions![0]!.params!.expression).toBe('{event.amount}*2');
    expect(saved.actions![1]!.params!.times).toBe('{event.amount}');
    const reopened = rebuilt(saved);
    expect(reopened.nodes.find((n) => n.type === 'reply.math')!.config.expression).toBe('{bits}*2');
  });

  test('a queue name survives, lowercased, and an empty one stays absent', () => {
    const drawn = { ...flow([trigger(), say('a')], [edge('t', 'next', 'a')]), queue: 'Sounds' };
    const saved = lowered(drawn);
    expect(saved.queue).toBe('sounds');
    expect(validateDefinition(saved)).toEqual([]);
    expect(rebuilt(saved).queue).toBe('sounds');
    expect('queue' in lowered({ ...drawn, queue: ' ' })).toBe(false);
    expect('queue' in rebuilt(lowered({ ...drawn, queue: '' }))).toBe(false);
  });

  test('a queued simple command stays an automation so the queue is kept', () => {
    const definition: Definition = { version: 1, queue: 'sounds', trigger: { type: 'command', params: { name: '!hi' } }, actions: [chat('a')] };
    expect(classifyFlow(definition).layer).toBe('automation');
  });

  test('a validator issue deep in a loop or API path lands on the drawn step', () => {
    const drawn = flow(
      [trigger(), loop('l'), get('g'), node('bad', 'reply.var', { do: 'set', name: 'no spaces' })],
      [edge('t', 'next', 'l'), edge('l', 'body', 'g'), edge('g', 'failure', 'bad')]
    );
    const issues = validateDefinition(lowered(drawn));
    expect(issues).toEqual([{ path: 'actions[0].params.actions[0].params.on_failure[0].params.name', code: 'simple_word' }]);
    expect(locateIssues(drawn, issues)).toEqual([{ path: 'nodes.bad.params.name', code: 'simple_word' }]);
  });

  test.each([
    { source: trigger(), port: 'next' },
    { source: node('c', 'cond.chance', { percent: 50 }), port: 'yes' },
    { source: say('s'), port: 'next' },
    { source: loop('l'), port: 'body' }
  ])('an oversized fork on $source.type $port belongs to the source node', ({ source, port }) => {
    const branches = Array.from({ length: 11 }, (_, i) => say(`branch-${i}`));
    const prefix = source.id === 't' ? [] : [trigger()];
    const incoming = prefix.map((root) => edge(root.id, 'next', source.id));
    const drawn = flow([...prefix, source, ...branches], [...incoming, ...branches.map((branch) => edge(source.id, port, branch.id))]);
    const issues = validateDefinition(lowered(drawn)).filter((issue) => issue.code === 'too_many');
    expect(locateIssues(drawn, issues)).toEqual([{ path: `nodes.${source.id}`, code: 'too_many' }]);
  });

  test('step refs link a run record index back to each nested step', () => {
    const drawn = flow(
      [trigger(), say('first'), loop('l'), get('g'), say('inner')],
      [edge('t', 'next', 'first'), edge('first', 'next', 'l'), edge('l', 'body', 'g'), edge('g', 'ok', 'inner')]
    );
    expect(flowStepRefs(drawn)).toEqual([
      { index: 0, type: 'chat', node: 'first', arm: '' },
      { index: 1, type: 'loop', node: 'l', arm: '' },
      { index: 1, type: 'http_get', node: 'g', arm: '/1.actions' },
      { index: 1, type: 'chat', node: 'inner', arm: '/1.actions/0.on_ok' }
    ]);
  });

  test('a disabled loop and a disabled API node stay disabled', () => {
    const definition: Definition = {
      version: 1,
      trigger: { type: 'event', params: { kind: 'channel.follow' } },
      actions: [
        { type: 'loop', enabled: false, params: { times: '2', actions: [chat('a')] } },
        { type: 'http_get', enabled: false, params: { url: 'https://api.example.com/x' } }
      ]
    };
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });
});

describe('reward trigger', () => {
  const managed = { title: 'Hydrate', cost: 500, color: '#00ccff', cooldown: 60, onRedeem: 'cancel', userInput: true };
  const settings = { title: 'Hydrate', cost: 500, color: '#00ccff', cooldown_s: 60, on_redeem: 'cancel', input_required: true };
  const trigger = (config: FlowConfig) => flow([node('t', 'twitch.reward', config)], []);

  test.each([
    ['a linked reward keeps only its id', { reward: 'r1' }, { reward_id: 'r1' }, { reward: 'r1' }],
    [
      'a saved managed reward carries its settings',
      { reward: 'r1', ...managed },
      { reward_id: 'r1', ...settings },
      { reward: 'r1', ...managed }
    ],
    [
      'an unset outcome writes fulfil and a stored blank draws as leave',
      { reward: 'r1', title: 'Hydrate', cost: 5 },
      { reward_id: 'r1', title: 'Hydrate', cost: 5, color: '', cooldown_s: 0, on_redeem: 'fulfill', input_required: false },
      { reward: 'r1', title: 'Hydrate', cost: 5, color: '', cooldown: 0, onRedeem: 'fulfill', userInput: false }
    ],
    [
      'a managed reward not yet created waits on the setup id',
      { reward: '', ...managed },
      { reward_id: SETUP_REWARD_ID, ...settings },
      { reward: '', ...managed }
    ]
  ] as [string, FlowConfig, Record<string, unknown>, FlowConfig][])('%s', (_name, config, params, drawn) => {
    const definition = lowered(trigger(config));
    expect(definition.trigger).toEqual({ type: 'reward', params });
    expect(rebuilt(definition).nodes[0]!.config).toEqual(drawn);
    expect(lowered(rebuilt(definition))).toEqual(definition);
  });

  test.each([
    ['a cost of zero', { ...managed, cost: 0 }, 'trigger.params.cost'],
    ['a bad colour', { ...managed, color: 'blue' }, 'trigger.params.color'],
    ['a title over 45 characters', { ...managed, title: 'x'.repeat(46) }, 'trigger.params.title'],
    ['a cooldown over a week', { ...managed, cooldown: 604801 }, 'trigger.params.cooldown_s'],
    ['an unknown outcome', { ...managed, onRedeem: 'refund' }, 'trigger.params.on_redeem']
  ] as [string, FlowConfig, string][])('%s is refused', (_name, config, path) => {
    const issues = validateDefinition(lowered(flow([node('t', 'twitch.reward', config), node('a', 'reply.chat', { message: 'hi' })], [edge('t', 'next', 'a')])));
    expect(issues.map((issue) => issue.path)).toEqual([path]);
  });
});

describe('shared joins', () => {
  const trigger = node('t', 'twitch.follow');
  const chance = (id: string) => node(id, 'cond.chance', { percent: 50 });
  const say = (id: string, message = 'x') => node(id, 'reply.chat', { message });

  test.each([
    {
      name: 'a check whose two outcomes start the same parallel paths',
      nodes: [trigger, chance('o'), chance('c'), say('a'), say('b'), say('j')],
      edges: [edge('t', 'next', 'o'), edge('o', 'yes', 'c'), edge('o', 'no', 'j'), edge('c', 'yes', 'a'), edge('c', 'yes', 'b'), edge('c', 'no', 'a'), edge('c', 'no', 'b'), edge('a', 'next', 'j'), edge('b', 'next', 'j')],
      last: chat('x')
    },
    {
      name: 'a loop body and a check after it that meet at one step',
      nodes: [trigger, node('l', 'reply.loop', { times: '2' }), chance('c'), say('j', 'JOIN')],
      edges: [edge('t', 'next', 'l'), edge('l', 'body', 'j'), edge('l', 'done', 'c'), edge('c', 'yes', 'j')],
      last: chat('JOIN')
    }
  ])('$name keeps the join unconditional', ({ nodes, edges, last }) => {
    expect(lowered(flow(nodes, edges)).actions.at(-1)).toEqual(last);
  });
});
