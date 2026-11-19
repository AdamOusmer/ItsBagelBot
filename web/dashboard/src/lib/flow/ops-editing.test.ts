// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { flowToDefinition, validateDefinition } from '@bagel/kit/automations';
import { newFlow, type Flow, type FlowEdge } from './model';
import { connect, deleteNode, insertNode, replaceTrigger, updateNodeConfig, type EdgeEnds, type Placement } from './ops';

type NodeRow = [id: string, type: string];
type EdgeRow = [from: string, port: string, to: string];
const at = { x: 72, y: 96 };

function graph(nodes: NodeRow[], edges: EdgeRow[]): Flow {
  return {
    ...newFlow('Edit'),
    nodes: nodes.map(([id, type]) => ({ id, type, x: 0, y: 0, config: { message: id } })),
    edges: edges.map(([from, port, to], index) => ({ id: `e${index}`, from, port, to }))
  };
}

const edgeRows = (flow: Flow): EdgeRow[] => flow.edges.map(({ from, port, to }): EdgeRow => [from, port, to]).sort();

const insertions: { name: string; type: string; placement: Placement; edges: EdgeRow[]; expected: EdgeRow[] }[] = [
  { name: 'canvas', type: 'reply.wait', placement: { kind: 'canvas' }, edges: [['t', 'next', 'a']], expected: [['t', 'next', 'a']] },
  { name: 'empty port', type: 'reply.wait', placement: { kind: 'port', from: 't', port: 'next', mode: 'insert' }, edges: [], expected: [['t', 'next', 'new']] },
  { name: 'parallel empty port', type: 'reply.wait', placement: { kind: 'port', from: 't', port: 'next', mode: 'parallel' }, edges: [], expected: [['t', 'next', 'new']] },
  {
    name: 'occupied port', type: 'reply.wait', placement: { kind: 'port', from: 't', port: 'next', mode: 'insert' },
    edges: [['t', 'next', 'a'], ['t', 'next', 'b']], expected: [['t', 'next', 'new'], ['new', 'next', 'a'], ['new', 'next', 'b']]
  },
  {
    name: 'parallel path', type: 'reply.wait', placement: { kind: 'port', from: 't', port: 'next', mode: 'parallel' },
    edges: [['t', 'next', 'a'], ['t', 'next', 'b']], expected: [['t', 'next', 'a'], ['t', 'next', 'b'], ['t', 'next', 'new']]
  },
  {
    name: 'condition continuation', type: 'cond.chance', placement: { kind: 'port', from: 't', port: 'next', mode: 'insert' },
    edges: [['t', 'next', 'a'], ['t', 'next', 'b']], expected: [['t', 'next', 'new'], ['new', 'yes', 'a'], ['new', 'yes', 'b']]
  },
  {
    name: 'loop continuation', type: 'reply.loop', placement: { kind: 'port', from: 't', port: 'next', mode: 'insert' },
    edges: [['t', 'next', 'a'], ['t', 'next', 'b']], expected: [['t', 'next', 'new'], ['new', 'done', 'a'], ['new', 'done', 'b']]
  },
  {
    name: 'one edge of a fork', type: 'reply.wait', placement: { kind: 'edge', edgeId: 'e0' },
    edges: [['t', 'next', 'a'], ['t', 'next', 'b']], expected: [['t', 'next', 'new'], ['new', 'next', 'a'], ['t', 'next', 'b']]
  },
  {
    name: 'API edge continuation', type: 'reply.http_get', placement: { kind: 'edge', edgeId: 'e0' },
    edges: [['t', 'next', 'a']], expected: [['t', 'next', 'new'], ['new', 'ok', 'a']]
  },
  {
    name: 'loop edge continuation', type: 'reply.loop', placement: { kind: 'edge', edgeId: 'e0' },
    edges: [['t', 'next', 'a']], expected: [['t', 'next', 'new'], ['new', 'done', 'a']]
  },
  {
    name: 'terminal edge insertion', type: 'reply.stop', placement: { kind: 'edge', edgeId: 'e0' },
    edges: [['t', 'next', 'a']], expected: [['t', 'next', 'new']]
  },
  {
    name: 'other ports stay intact', type: 'reply.wait', placement: { kind: 'port', from: 'g', port: 'yes', mode: 'insert' },
    edges: [['t', 'next', 'g'], ['g', 'yes', 'a'], ['g', 'no', 'b']], expected: [['t', 'next', 'g'], ['g', 'yes', 'new'], ['new', 'next', 'a'], ['g', 'no', 'b']]
  }
];

describe('insertNode', () => {
  test.each(insertions)('$name', ({ type, placement, edges, expected }) => {
    const flow = graph([['t', 'twitch.follow'], ['g', 'cond.chance'], ['a', 'reply.chat'], ['b', 'reply.chat']], edges);
    const node = insertNode(flow, { type, at, placement });
    const renamed = { ...flow, edges: flow.edges.map((edge) => ({ ...edge, from: edge.from === node.id ? 'new' : edge.from, to: edge.to === node.id ? 'new' : edge.to })) };
    expect({ type: node.type, x: node.x, y: node.y, edges: edgeRows(renamed) }).toEqual({ type, ...at, edges: [...expected].sort() });
    expect(flow.nodes.filter((item) => item.id === node.id)).toHaveLength(1);
  });

  test.each(insertions.slice(0, 3))('triggers cannot be inserted at $name', ({ placement }) => {
    const flow = graph([['t', 'twitch.follow'], ['a', 'reply.chat']], [['t', 'next', 'a']]);
    const before = structuredClone(flow);
    expect(() => insertNode(flow, { type: 'twitch.sub', at, placement })).toThrow('replaceTrigger');
    expect(flow).toEqual(before);
  });
});

const deletions: { name: string; type: string; edges: EdgeRow[]; removed: string[]; healed: boolean; remaining: EdgeRow[] }[] = [
  { name: 'linear middle', type: 'reply.wait', edges: [['t', 'next', 'x'], ['x', 'next', 'a']], removed: ['x'], healed: true, remaining: [['t', 'next', 'a']] },
  {
    name: 'shared junction prunes unreachable children', type: 'reply.wait',
    edges: [['t', 'next', 'p'], ['t', 'next', 'x'], ['t', 'other', 'x'], ['p', 'next', 'x'], ['x', 'next', 'a'], ['x', 'next', 'b'], ['t', 'next', 'a']],
    removed: ['x', 'b'], healed: false,
    remaining: [['t', 'next', 'p'], ['t', 'next', 'a']]
  },
  { name: 'terminal', type: 'reply.wait', edges: [['t', 'next', 'x']], removed: ['x'], healed: true, remaining: [] },
  { name: 'empty check', type: 'cond.chance', edges: [['t', 'next', 'x']], removed: ['x'], healed: true, remaining: [] },
  { name: 'only first check port', type: 'cond.chance', edges: [['t', 'next', 'x'], ['x', 'yes', 'a'], ['x', 'yes', 'b']], removed: ['x'], healed: true, remaining: [['t', 'next', 'a'], ['t', 'next', 'b']] },
  { name: 'only API success', type: 'reply.http_get', edges: [['t', 'next', 'x'], ['x', 'ok', 'a']], removed: ['x'], healed: true, remaining: [['t', 'next', 'a']] },
  { name: 'only loop body', type: 'reply.loop', edges: [['t', 'next', 'x'], ['x', 'body', 'a']], removed: ['x'], healed: true, remaining: [['t', 'next', 'a']] },
  { name: 'only loop done', type: 'reply.loop', edges: [['t', 'next', 'x'], ['x', 'done', 'a']], removed: ['x'], healed: true, remaining: [['t', 'next', 'a']] },
  { name: 'only check second port', type: 'cond.chance', edges: [['t', 'next', 'x'], ['x', 'no', 'a'], ['a', 'next', 'b']], removed: ['x', 'a', 'b'], healed: false, remaining: [] },
  { name: 'only API failure', type: 'reply.http_post', edges: [['t', 'next', 'x'], ['x', 'failure', 'a']], removed: ['x', 'a'], healed: false, remaining: [] },
  { name: 'both check ports', type: 'cond.chance', edges: [['t', 'next', 'x'], ['x', 'yes', 'a'], ['x', 'no', 'b']], removed: ['x', 'a', 'b'], healed: false, remaining: [] },
  { name: 'both loop ports', type: 'reply.loop', edges: [['t', 'next', 'x'], ['x', 'body', 'a'], ['x', 'done', 'b']], removed: ['x', 'a', 'b'], healed: false, remaining: [] },
  {
    name: 'shared path survives', type: 'cond.chance',
    edges: [['t', 'next', 'x'], ['t', 'next', 'p'], ['x', 'yes', 'a'], ['x', 'no', 'b'], ['a', 'next', 'c'], ['p', 'next', 'b'], ['b', 'next', 'c'], ['lone', 'next', 'orphan']],
    removed: ['x', 'a'], healed: false, remaining: [['t', 'next', 'p'], ['p', 'next', 'b'], ['b', 'next', 'c'], ['lone', 'next', 'orphan']]
  },
  {
    name: 'disconnected descendants stay', type: 'cond.chance',
    edges: [['x', 'yes', 'a'], ['x', 'no', 'b'], ['a', 'next', 'c']], removed: ['x'], healed: false, remaining: [['a', 'next', 'c']]
  },
  { name: 'trigger preserves all children', type: 'twitch.sub', edges: [['x', 'next', 'a'], ['a', 'next', 'b']], removed: ['x'], healed: false, remaining: [['a', 'next', 'b']] }
];

describe('deleteNode', () => {
  test.each<{ name: string; edges: EdgeRow[]; expected: string[] }>([
    { name: 'before sibling', edges: [['t', 'next', 'x'], ['t', 'next', 'p'], ['x', 'next', 'a'], ['x', 'next', 'b']], expected: ['a', 'b', 'p'] },
    { name: 'after sibling', edges: [['t', 'next', 'p'], ['t', 'next', 'x'], ['x', 'next', 'a'], ['x', 'next', 'b']], expected: ['p', 'a', 'b'] }
  ])('healed edges preserve parallel order $name', ({ edges, expected }) => {
    const flow = graph([['t', 'twitch.follow'], ['x', 'reply.wait'], ['p', 'reply.chat'], ['a', 'reply.chat'], ['b', 'reply.chat']], edges);
    deleteNode(flow, 'x');
    expect(flow.edges.filter((edge) => edge.from === 't').map((edge) => edge.to)).toEqual(expected);
  });

  test.each(deletions)('$name', ({ type, edges, removed, healed, remaining }) => {
    const flow = graph([['t', 'twitch.follow'], ['x', type], ['p', 'reply.chat'], ['a', 'reply.chat'], ['b', 'reply.chat'], ['c', 'reply.chat'], ['lone', 'reply.chat'], ['orphan', 'reply.chat']], edges);
    const original = flow.nodes.map((node) => node.id);
    const result = deleteNode(flow, 'x');
    expect({ result, nodes: flow.nodes.map((node) => node.id), edges: edgeRows(flow) }).toEqual({
      result: { removed, healed }, nodes: original.filter((id) => !removed.includes(id)), edges: [...remaining].sort()
    });
  });

  test('unknown node leaves the graph intact', () => {
    const flow = graph([['t', 'twitch.follow']], []);
    const before = structuredClone(flow);
    expect({ result: deleteNode(flow, 'missing'), flow }).toEqual({ result: { removed: [], healed: false }, flow: before });
  });
});

test.each([
  { name: 'new trigger', previous: null },
  { name: 'existing trigger', previous: 'twitch.chat.command' },
  { name: 'same trigger type', previous: 'twitch.sub' }
])('replaceTrigger $name resets config and keeps outgoing edges', ({ previous }) => {
  const nodes: NodeRow[] = previous ? [['root', previous], ['a', 'reply.chat'], ['b', 'reply.chat']] : [['a', 'reply.chat'], ['b', 'reply.chat']];
  const flow = graph(nodes, previous ? [['root', 'next', 'a'], ['root', 'next', 'b']] : []);
  const original = flow.nodes.find((node) => node.id === 'root');
  if (original) Object.assign(original, { x: 72, y: 96, config: { trigger: '!old', variant: 'gift', disabled: true } });
  const beforeEdges = structuredClone(flow.edges);
  const node = replaceTrigger(flow, 'twitch.sub');
  expect({ config: node.config, type: node.type, edges: flow.edges, count: flow.nodes.length }).toEqual({ config: { variant: 'any' }, type: 'twitch.sub', edges: beforeEdges, count: 3 });
  expect(original ? node === original : flow.nodes.includes(node)).toBe(true);
  expect({ id: node.id, x: node.x, y: node.y }).toEqual(original ? { id: 'root', ...at } : { id: node.id, x: 0, y: 0 });
});

const connections: { name: string; ends: EdgeEnds; edges: EdgeRow[]; accepted: boolean }[] = [
  { name: 'new path', ends: { from: 'b', port: 'next', to: 'c' }, edges: [['t', 'next', 'a'], ['a', 'next', 'b']], accepted: true },
  { name: 'self loop', ends: { from: 'a', port: 'next', to: 'a' }, edges: [], accepted: false },
  { name: 'short cycle', ends: { from: 'b', port: 'next', to: 'a' }, edges: [['a', 'next', 'b']], accepted: false },
  { name: 'long cycle', ends: { from: 'c', port: 'next', to: 'a' }, edges: [['a', 'yes', 'b'], ['b', 'next', 'c']], accepted: false },
  { name: 'disconnected cycle', ends: { from: 'c', port: 'next', to: 'a' }, edges: [['a', 'yes', 'b'], ['a', 'no', 'c'], ['b', 'next', 'c']], accepted: false },
  { name: 'duplicate edge', ends: { from: 'a', port: 'next', to: 'b' }, edges: [['a', 'next', 'b']], accepted: false },
  { name: 'trigger target', ends: { from: 'a', port: 'next', to: 't' }, edges: [], accepted: false },
  { name: 'shared child', ends: { from: 'a', port: 'no', to: 'c' }, edges: [['a', 'yes', 'b'], ['b', 'next', 'c']], accepted: true }
];

test.each([
  { prior: '3', next: '2', kept: ['p1', 'p2'] },
  { prior: '3', next: '5', kept: ['p1', 'p2', 'p3'] }
])('changing random paths from $prior to $next keeps $kept', ({ prior, next, kept }) => {
  const flow = graph(
    [['t', 'twitch.follow'], ['r', 'reply.random'], ['a', 'reply.chat'], ['b', 'reply.chat'], ['c', 'reply.chat']],
    [['t', 'next', 'r'], ['r', 'p1', 'a'], ['r', 'p2', 'b'], ['r', 'p3', 'c']]
  );
  const node = flow.nodes[1]!;
  node.config = { paths: prior, weight3: 9 };
  updateNodeConfig(flow, node, { paths: next });
  expect({ config: node.config, ports: flow.edges.map(({ port }) => port), nodes: flow.nodes.map(({ id }) => id) })
    .toEqual({ config: { paths: next, weight3: 9 }, ports: ['next', ...kept], nodes: ['t', 'r', 'a', 'b', 'c'] });
});

test.each(connections)('connect $name', ({ ends, edges, accepted }) => {
  const flow = graph([['t', 'twitch.follow'], ['a', 'cond.chance'], ['b', 'reply.chat'], ['c', 'reply.chat']], edges);
  const before = structuredClone(flow.edges);
  const edge = connect(flow, ends);
  expect(edge !== null).toBe(accepted);
  const added: FlowEdge[] = edge ? [edge] : [];
  expect(flow.edges).toEqual([...before, ...added]);
});

test('inserting and deleting through the public API keeps a linear recipe saveable', () => {
  const flow = graph([['t', 'twitch.follow'], ['a', 'reply.chat']], [['t', 'next', 'a']]);
  const node = insertNode(flow, { type: 'reply.wait', at, placement: { kind: 'edge', edgeId: 'e0' } });
  const edited = flowToDefinition(flow);
  if (!edited.ok) throw new Error(JSON.stringify(edited.issues));
  expect({ actions: edited.value.actions, issues: validateDefinition(edited.value) }).toEqual({ actions: [{ type: 'delay', params: { ms: 5000 } }, { type: 'chat', params: { text: 'a' } }], issues: [] });
  deleteNode(flow, node.id);
  const saved = flowToDefinition(flow);
  if (!saved.ok) throw new Error(JSON.stringify(saved.issues));
  expect({ actions: saved.value.actions, issues: validateDefinition(saved.value) }).toEqual({ actions: [{ type: 'chat', params: { text: 'a' } }], issues: [] });
});
