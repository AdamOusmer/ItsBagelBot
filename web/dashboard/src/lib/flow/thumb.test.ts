// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import type { Flow } from './model';
import { flowPreview } from './thumb';

const n = (id: string, type: string) => ({ id, type, x: 0, y: 0, config: {} });
const flow = (nodes: Flow['nodes'], edges: Flow['edges']): Flow => ({ id: 'x', name: 'x', enabled: true, nodes, edges });

describe('flowPreview', () => {
  const hug = flow(
    [n('a', 'twitch.chat.command'), n('b', 'cond.chance'), n('c', 'reply.chat'), n('d', 'reply.chat')],
    [
      { id: '1', from: 'a', port: 'next', to: 'b' },
      { id: '2', from: 'b', port: 'yes', to: 'c' },
      { id: '3', from: 'b', port: 'no', to: 'd' }
    ]
  );

  test('boxes carry kind, start at the origin, and read left to right', () => {
    const { n: boxes } = flowPreview(hug);
    expect(boxes.map((b) => b[3])).toEqual([0, 1, 2, 2]);
    expect(Math.min(...boxes.map((b) => b[0]))).toBe(0);
    expect(Math.min(...boxes.map((b) => b[1]))).toBe(0);
    expect(boxes[1][0]).toBeGreaterThan(boxes[0][0]);
  });

  test('wires point at box indexes and mark the second branch', () => {
    expect(flowPreview(hug).e).toEqual([[0, 1, 0], [1, 2, 0], [1, 3, 1]]);
  });

  test('stays small enough to store on the row', () => {
    expect(JSON.stringify(flowPreview(hug)).length).toBeLessThan(120);
  });

  test('a wire to a missing step is dropped', () => {
    const broken = flow([n('a', 'twitch.follow')], [{ id: '1', from: 'a', port: 'next', to: 'gone' }]);
    expect(flowPreview(broken).e).toEqual([]);
  });

  test('an empty flow previews as nothing', () => {
    expect(flowPreview(flow([], []))).toEqual({ n: [], e: [] });
  });
});
