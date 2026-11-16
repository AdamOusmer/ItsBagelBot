// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { MAX_ACTIONS } from '@bagel/kit/automations';
import { tidy } from './layout';
import { newFlow, type Flow } from './model';

test.each([300, MAX_ACTIONS])('tidy preserves a %i-step chain when edges are reversed', (count) => {
  const nodes = Array.from({ length: count + 1 }, (_, index) => ({ id: String(index), type: index === 0 ? 'twitch.follow' : 'reply.chat', config: {}, x: 0, y: 0 }));
  const edges = nodes.slice(1).map((node, index) => ({ id: node.id, from: nodes[index].id, port: 'next', to: node.id }));
  const flow: Flow = { ...newFlow('Chain'), nodes, edges: edges.toReversed() };
  const positions = tidy(flow);
  expect([positions.size, positions]).toEqual([nodes.length, tidy({ ...flow, edges })]);
});
