// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { addStep, connectionTarget, parallelPorts, portOccupied, removeStep, selectedPlacement, selectionAfterHistory } from './editor';
import { FlowHistory } from './history';
import { newFlow, type Flow } from './model';
import { stepIndex } from './keyboard';
import { connect } from './ops';

function recipe(type = 'reply.wait'): Flow {
  return {
    ...newFlow('Editor'),
    nodes: [['t', 'twitch.follow'], ['a', type], ['b', 'reply.chat']].map(([id, type]) => ({ id, type, x: 0, y: 0, config: {} })),
    edges: [{ id: 'ta', from: 't', port: 'next', to: 'a' }, { id: 'ab', from: 'a', port: 'next', to: 'b' }]
  };
}

test.each([
  { selected: null, expected: { kind: 'canvas' } },
  { selected: 'missing', expected: { kind: 'canvas' } },
  { selected: 't', expected: { kind: 'port', from: 't', port: 'next', mode: 'insert' } },
  { selected: 'a', expected: { kind: 'port', from: 'a', port: 'next', mode: 'insert' } }
])('library and drop placement for $selected', ({ selected, expected }) => {
  expect(selectedPlacement(recipe(), selected)).toEqual(expected);
});

test.each([
  { type: 'reply.wait', port: 'next' },
  { type: 'cond.chance', port: 'yes' },
  { type: 'reply.loop', port: 'body' },
  { type: 'reply.http_get', port: 'ok' },
  { type: 'reply.random', port: 'p1' }
])('inserts after selected $type', ({ type, port }) => {
  const flow = recipe(type);
  flow.edges[1].port = port;
  const { node } = addStep(flow, { type: 'reply.wait', at: { x: 360, y: 0 }, placement: selectedPlacement(flow, 'a') });
  expect(flow.edges.map(({ from, port, to }) => ({ from, port, to }))).toEqual([
    { from: 't', port: 'next', to: 'a' },
    { from: 'a', port, to: node.id },
    { from: node.id, port: 'next', to: 'b' }
  ]);
});

test.each([
  { existing: true },
  { existing: false }
])('trigger insertion, existing=$existing', ({ existing }) => {
  const flow = recipe();
  if (!existing) removeStep(flow, 't');
  const before = structuredClone(flow.edges);
  const { node, replaced } = addStep(flow, { type: 'twitch.sub', at: { x: 360, y: 24 }, placement: selectedPlacement(flow, 'a') });
  expect({ replaced, edges: flow.edges, triggers: flow.nodes.filter((node) => node.type === 'twitch.sub'), at: { x: node.x, y: node.y } }).toEqual({
    replaced: existing, edges: before, triggers: [node], at: existing ? { x: 0, y: 0 } : { x: 360, y: 24 }
  });
});

test.each([
  { id: 'a', parent: 't', count: 1 },
  { id: 't', parent: 'a', count: 1 },
  { id: 'missing', parent: null, count: 0 }
])('deletion and history selection for $id', ({ id, parent, count }) => {
  const flow = recipe();
  const history = new FlowHistory(flow);
  expect(removeStep(flow, id)).toEqual({ parent, count });
  history.sync(flow);
  const before = new Set(flow.nodes.map((node) => node.id));
  history.undo(flow);
  expect(selectionAfterHistory(flow, before, parent)).toBe(count ? id : null);
  history.redo(flow);
  expect(selectionAfterHistory(flow, new Set(flow.nodes.map((node) => node.id)), parent)).toBe(parent);
});

test.each([
  { fork: false, occupied: true, ports: [] },
  { fork: true, occupied: true, ports: ['next'] }
])('occupied port and parallel pill, fork=$fork', ({ fork, occupied, ports }) => {
  const flow = recipe();
  if (fork) flow.edges.push({ id: 'tb', from: 't', port: 'next', to: 'b' });
  expect({ occupied: portOccupied(flow, { from: 't', port: 'next' }), ports: parallelPorts(flow, 't'), empty: portOccupied(flow, undefined) }).toEqual({ occupied, ports: [...ports], empty: false });
});

test.each([
  { type: 'reply.stop', paths: '2', port: 'next', placement: { kind: 'canvas' }, ports: [], connected: false },
  { type: 'reply.random', paths: '2', port: 'p3', placement: { kind: 'port', from: 'a', port: 'p1', mode: 'insert' }, ports: [], connected: false },
  { type: 'reply.random', paths: '3', port: 'p3', placement: { kind: 'port', from: 'a', port: 'p1', mode: 'insert' }, ports: ['p3'], connected: true }
])('$type with $paths paths exposes only available ports', ({ type, paths, port, placement, ports, connected }) => {
  const flow = recipe(type);
  flow.nodes.find(({ id }) => id === 'a')!.config.paths = paths;
  flow.edges[1]!.port = port;
  flow.edges.push({ id: 'ac', from: 'a', port, to: 'c' });
  flow.nodes.push(...['c', 'd'].map((id) => ({ id, type: 'reply.chat', config: {}, x: 0, y: 0 })));
  const result = { placement: selectedPlacement(flow, 'a'), ports: parallelPorts(flow, 'a'), connected: connectionTarget(flow, 'd', { from: 'a', port }) };
  expect(result).toEqual({ placement, ports, connected });
});
test.each([
  { type: 'cond.chance', ports: ['yes', 'no'] },
  { type: 'reply.http_get', ports: ['ok', 'failure'] }
])('branched $type deletion counts descendants and restores the removed root selection', ({ type, ports }) => {
  const flow = recipe(type);
  flow.nodes.unshift({ id: 'c', type: 'reply.chat', x: 0, y: 0, config: {} });
  flow.edges[1].port = ports[0];
  flow.edges.push({ id: 'ac', from: 'a', port: ports[1], to: 'c' });
  const history = new FlowHistory(flow);
  expect(removeStep(flow, 'a')).toEqual({ parent: 't', count: 3 });
  history.sync(flow);
  const before = new Set(flow.nodes.map((node) => node.id));
  history.undo(flow);
  expect(selectionAfterHistory(flow, before, 't')).toBe('a');
});

test.each([
  { key: 'ArrowDown', index: 0, length: 3, expected: 1 },
  { key: 'ArrowDown', index: 2, length: 3, expected: 0 },
  { key: 'ArrowUp', index: 0, length: 3, expected: 2 },
  { key: 'ArrowUp', index: 2, length: 3, expected: 1 },
  { key: 'ArrowDown', index: 0, length: 0, expected: -1 }
])('picker $key from $index in $length results', ({ expected, ...navigation }) => {
  expect(stepIndex(navigation.index, navigation.length, navigation.key)).toBe(expected);
});

test('library add through the selected trigger keeps its existing continuation', () => {
  const flow = recipe();
  const { node } = addStep(flow, { type: 'reply.wait', at: { x: 0, y: 0 }, placement: selectedPlacement(flow, 't') });
  expect(flow.edges.map(({ from, to }) => [from, to])).toEqual([['a', 'b'], ['t', node.id], [node.id, 'a']]);
});

test.each([
  { name: 'cycle', from: 'b', to: 'a', accepted: false },
  { name: 'duplicate', from: 't', to: 'a', accepted: false },
  { name: 'self', from: 'a', to: 'a', accepted: false },
  { name: 'trigger', from: 'a', to: 't', accepted: false },
  { name: 'missing source', from: 'gone', to: 'a', accepted: false },
  { name: 'missing target', from: 'a', to: 'gone', accepted: false },
  { name: 'forward', from: 't', to: 'b', accepted: true }
])('connection highlight and acceptance agree for $name', ({ from, to, accepted }) => {
  const flow = recipe();
  const source = { from, port: 'next' };
  const highlighted = connectionTarget(flow, to, source);
  expect({ highlighted, connected: Boolean(connect(flow, { ...source, to })) }).toEqual({ highlighted: accepted, connected: accepted });
});

test.each<{ nodes: string[]; expected: string | null }>([
  { nodes: ['t'], expected: null },
  { nodes: ['t', 'a', 'b'], expected: 'a' }
])('trigger deletion chooses a surviving canvas focus target in $nodes', ({ nodes, expected }) => {
  const flow = recipe();
  flow.nodes = flow.nodes.filter((node) => nodes.includes(node.id));
  flow.edges = flow.edges.filter((edge) => nodes.includes(edge.from) && nodes.includes(edge.to));
  expect(removeStep(flow, 't')).toEqual({ parent: expected, count: 1 });
});
