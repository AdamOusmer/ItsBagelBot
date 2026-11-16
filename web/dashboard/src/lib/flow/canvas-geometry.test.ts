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

test.each([
  { width: 340, left: 1032 - 340 / 0.7 },
  { width: 480, left: 368 },
  { width: 600, left: 368 },
  { width: 900, left: 0 }
])('focus includes only steps that fit at readable zoom in $width pixels', ({ width, left }) => {
  const flow = chain(3);
  flow.nodes.push({ id: 'sibling', type: 'reply.chat', x: 736, y: 2000, config: {} });
  flow.edges.push({ id: 'sibling', from: '1', to: 'sibling', port: 'next' });
  const box = focusBounds(flow, flow.nodes[2], { width, height: 600, minZoom: 0.7 });
  expect([box.x0, box.y1 < 600]).toEqual([left, true]);
});

test.each([1440, 1024, 834, 390])('reveals readable steps within a %i frame without moving a visible recipe', (width) => {
  const flow = chain(3);
  const inset = { left: width < 500 ? 16 : 104, right: width >= 1280 ? 408 : 16, top: 100, bottom: 200 };
  const size = { width: width < 500 ? width : width - 240, height: 900, inset };
  const box = focusBounds(flow, flow.nodes[2], { width: size.width - inset.left - inset.right, height: 600, minZoom: 0.7 });
  const view = revealBox({ x: inset.left, y: 200, k: 0.7 }, box, size, { min: 0.4, max: 1.6, readable: 0.7 });
  expect([view.x + box.x0 * view.k >= inset.left - 0.001, view.x + box.x1 * view.k <= size.width - inset.right + 0.001, view.y, view.k >= 0.7]).toEqual([true, true, 200, true]);
  expect(revealBox(view, box, size, { min: 0.4, max: 1.6, readable: view.k })).toEqual(view);
});

test.each([
  { height: 35, whole: false },
  { height: 78, whole: false },
  { height: 188, whole: true },
  { height: 213, whole: true }
])('short touch frames prioritize the title, then the card and ports in $height pixels', ({ height, whole }) => {
  const flow = chain(2);
  const node = { ...flow.nodes[1], type: 'cond.compare' };
  const box = focusBounds(flow, node, { width: 288, height, minZoom: 0.7, touch: true });
  const frame = { width: 320, height: height + 203, inset: { left: 16, right: 16, top: 203, bottom: 0 } };
  const view = revealBox({ x: 0, y: 0, k: 0.7 }, box, frame, { min: 0.1, max: 1.6, readable: 0.7 });
  expect({ whole: box.y1 > node.y + 60, title: view.y + (node.y + 36) * view.k <= frame.height, left: view.x + box.x0 * view.k >= 16 }).toEqual({ whole, title: true, left: true });
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
