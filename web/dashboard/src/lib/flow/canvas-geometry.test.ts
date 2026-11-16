// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { focusBounds, windowEdges, windowNodes } from './canvas-geometry';
import { newFlow, type Flow } from './model';
import { revealBox } from './touch';

const chain = (count: number): Flow => ({
  ...newFlow('Test'),
  nodes: Array.from({ length: count }, (_, i) => ({ id: String(i), type: i === 0 ? 'twitch.follow' : 'reply.chat', config: {}, x: i * 368, y: 0 })),
  edges: Array.from({ length: count - 1 }, (_, i) => ({ id: String(i), from: String(i), to: String(i + 1), port: 'next' }))
});

test.each([1440, 1024, 834])('reveals parent and new step within a %i frame without moving a visible recipe', (width) => {
  const flow = chain(3);
  const inset = { left: 104, right: width >= 1280 ? 408 : 16, top: 100, bottom: 200 };
  const size = { width: width - 240, height: 900, inset };
  const box = focusBounds(flow, flow.nodes[2], { width: size.width - inset.left - inset.right, height: 600, minZoom: 0.4 });
  const view = revealBox({ x: inset.left, y: 200, k: 0.7 }, box, size, { min: 0.4, max: 1.6, readable: 0.7 });
  expect([view.x + box.x0 * view.k >= inset.left - 0.001, view.x + box.x1 * view.k <= size.width - inset.right + 0.001, view.y]).toEqual([true, true, 200]);
  expect(revealBox(view, box, size, { min: 0.4, max: 1.6, readable: view.k })).toEqual(view);
});

test.each([0, 180000, 360000])('large boards render the viewport at %i and retain selected steps', (pan) => {
  const flow = chain(1001);
  const visible = windowNodes(flow, { x: -pan, y: 0, k: 1 }, { width: 1000, height: 900 }, '1000');
  const expected = flow.nodes.filter((node) => node.id === '1000' || (node.x <= pan + 1400 && node.x + 288 >= pan - 400));
  expect(visible.map((node) => node.id)).toEqual(expected.map((node) => node.id));
  expect(flow.nodes).toHaveLength(1001);
});

test('large boards keep a wire that crosses the viewport between two far steps', () => {
  const flow = chain(101);
  flow.nodes[1] = { ...flow.nodes[1], x: 4000 };
  const edges = windowEdges(flow, { x: -2000, y: 0, k: 1 }, { width: 1000, height: 900 });
  expect(edges.has('0')).toBe(true);
});
