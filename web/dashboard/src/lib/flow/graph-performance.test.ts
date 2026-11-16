// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { connectionTarget } from './editor';
import { edgePaths } from './canvas-geometry';
import { newFlow, variablesFor, type Flow } from './model';

function chain(): { flow: Flow; reads: () => number } {
  let reads = 0;
  const flow = newFlow('Large');
  flow.nodes = Array.from({ length: 1002 }, (_, index) => ({
    id: String(index), type: index === 0 ? 'twitch.follow' : 'reply.math', x: 0, y: 0,
    config: { saveAs: String.fromCharCode(97 + Math.floor(index / 676), 97 + Math.floor(index / 26) % 26, 97 + index % 26) }
  }));
  flow.edges = flow.nodes.slice(1).map((node, index) => ({
    id: node.id, get from() { reads++; return String(index); }, port: 'next', to: node.id
  }));
  return { flow, reads: () => reads };
}

test.each(['scope', 'targets', 'wires'] as const)('large graph %s reads each edge a bounded number of times', (operation) => {
  const { flow, reads } = chain();
  const results = {
    scope: () => variablesFor(flow, '1001').length,
    targets: () => flow.nodes.filter((node) => connectionTarget(flow, node.id, { from: '500', port: 'next' })).length,
    wires: () => edgePaths(flow, {}).length
  };
  const expected = { scope: 1002, targets: 500, wires: 1001 };
  expect([results[operation](), reads() <= flow.edges.length * 8]).toEqual([expected[operation], true]);
});

test('target checks reuse their source exclusions until edges change', () => {
  const { flow, reads } = chain();
  flow.nodes.forEach((node) => connectionTarget(flow, node.id, { from: '500', port: 'next' }));
  const initial = reads();
  flow.nodes.forEach((node) => connectionTarget(flow, node.id, { from: '500', port: 'next' }));
  expect(reads()).toBe(initial);
  flow.edges = flow.edges.filter((edge) => edge.to !== '500');
  expect(connectionTarget(flow, '0', { from: '500', port: 'next' })).toBe(false);
  expect(connectionTarget(flow, '1', { from: '500', port: 'next' })).toBe(true);
});
