// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { inputY, loopTails, portY, tidy } from './layout';
import { specFor, type Flow } from './model';

const n = (id: string, type: string) => ({ id, type, x: 0, y: 0, config: {} });

function wireDrop(flow: Flow, at: ReturnType<typeof tidy>, edgeId: string): number {
  const edge = flow.edges.find((e) => e.id === edgeId)!;
  const from = flow.nodes.find((node) => node.id === edge.from)!;
  const out = (at.get(edge.from)?.y ?? 0) + portY(specFor(from.type), edge.port);
  return (at.get(edge.to)?.y ?? 0) + inputY - out;
}

describe('loopTails', () => {
  const node = (id: string, type: string) => ({ id, type, x: 0, y: 0, config: {} });
  const edge = (from: string, port: string, to: string) => ({ id: `${from}${port}${to}`, from, port, to });

  test('each open end of a body returns to its loop, and the path after the loop does not', () => {
    const flow: Flow = {
      id: 'f',
      name: 'f',
      enabled: true,
      nodes: [node('l', 'reply.loop'), node('g', 'reply.http_get'), node('a', 'reply.chat'), node('b', 'reply.chat'), node('after', 'reply.chat')],
      edges: [edge('l', 'body', 'g'), edge('g', 'ok', 'a'), edge('g', 'failure', 'b'), edge('l', 'done', 'after')]
    };
    expect(loopTails(flow)).toEqual([
      { loop: 'l', tail: 'a' },
      { loop: 'l', tail: 'b' }
    ]);
  });

  test('a body wired onto the step after the loop has no return of its own', () => {
    const flow: Flow = {
      id: 'f',
      name: 'f',
      enabled: true,
      nodes: [node('l', 'reply.loop'), node('a', 'reply.chat'), node('after', 'reply.chat')],
      edges: [edge('l', 'body', 'a'), edge('a', 'next', 'after'), edge('l', 'done', 'after')]
    };
    expect(loopTails(flow)).toEqual([]);
  });
});

describe('tidy', () => {
  test('a lone next step lines up so its wire runs straight', () => {
    const flow: Flow = {
      id: 'x',
      name: 'x',
      enabled: true,
      nodes: [n('a', 'twitch.follow'), n('b', 'cond.chance'), n('c', 'reply.chat')],
      edges: [
        { id: '1', from: 'a', port: 'next', to: 'b' },
        { id: '2', from: 'b', port: 'yes', to: 'c' }
      ]
    };
    const at = tidy(flow);
    expect(wireDrop(flow, at, '1')).toBe(0);
    expect(wireDrop(flow, at, '2')).toBe(0);
  });

  test('branches sit either side of their condition without touching', () => {
    const flow: Flow = {
      id: 'x',
      name: 'x',
      enabled: true,
      nodes: [n('a', 'cond.chance'), n('b', 'reply.chat'), n('c', 'reply.chat')],
      edges: [
        { id: '1', from: 'a', port: 'yes', to: 'b' },
        { id: '2', from: 'a', port: 'no', to: 'c' }
      ]
    };
    const at = tidy(flow);
    expect(at.get('c')!.y).toBeGreaterThan(at.get('b')!.y + 84);
  });
});
