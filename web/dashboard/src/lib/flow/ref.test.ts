// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { flowRouteId, parseFlowRouteId, type FlowRef } from './ref';

describe('flow route ids', () => {
  const refs: FlowRef[] = [
    { layer: 'automation', id: '0f8c-aa11' },
    { layer: 'command', name: 'hug' },
    { layer: 'command', name: 'café & co/1' },
    { layer: 'alert', alert: 'raid' }
  ];

  test.each(refs)('round trips %o', (ref) => {
    expect(parseFlowRouteId(flowRouteId(ref))).toEqual(ref);
  });

  test('keeps the route to one path segment', () => {
    expect(flowRouteId({ layer: 'command', name: 'a/b' })).not.toContain('/');
  });

  test.each(['', 'x', 'a-', 'l-nothing', 'q-1', 'c-%E0%A4%A', 'new'])('rejects %p', (id) => {
    expect(parseFlowRouteId(id)).toBeNull();
  });
});
