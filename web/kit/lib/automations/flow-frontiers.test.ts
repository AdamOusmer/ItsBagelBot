// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { flowToDefinition, type Flow, type FlowNode } from './flow-graph';
import { validateDefinition } from './validate';

test.each([3, 192])('shared frontiers across %i pairs stop at their join boundary', (count) => {
  const root: FlowNode = { id: 't', type: 'twitch.follow', config: {}, x: 0, y: 0 };
  const pairs = Array.from({ length: count }, (_, i) => ['a', 'b'].map((side): FlowNode => ({
    id: `${i}${side}`, type: 'reply.chat', config: { message: 'x' }, x: 0, y: 0
  })));
  const edges = pairs.flatMap((pair, i) => (pairs[i - 1] ?? [root]).flatMap((from) => pair.map((to) => ({
    id: `${from.id}:${to.id}`, from: from.id, to: to.id, port: 'next'
  }))));
  const flow: Flow = { id: 'f', name: 'Frontiers', enabled: true, nodes: [root, ...pairs.flat()], edges };
  const result = flowToDefinition(flow);
  expect(result.ok).toBe(true);
  if (!result.ok) return;
  expect(validateDefinition(result.value)).toEqual([]);
  expect(result.value.actions).toHaveLength(count);
});
