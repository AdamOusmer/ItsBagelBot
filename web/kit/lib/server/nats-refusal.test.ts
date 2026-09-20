// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';

mock.module('newrelic', () => ({
  default: {
    startSegment: (_name: string, _record: boolean, run: () => unknown) => run(),
    recordMetric: () => {}
  }
}));
const { RpcError, rpcRefusal } = await import('./nats');

describe('rpcRefusal', () => {
  test('a reply without an error is not a refusal', () => {
    expect(rpcRefusal({ ok: true })).toBeNull();
    expect(rpcRefusal(null)).toBeNull();
    expect(rpcRefusal({ error: '' })).toBeNull();
  });

  test('a refusal keeps the message and the shared code', () => {
    const r = rpcRefusal({ error: 'no such board', code: 'not_found' });
    expect(r).toBeInstanceOf(RpcError);
    expect(r?.message).toBe('no such board');
    expect(r?.code).toBe('not_found');
  });

  test('a code outside the shared vocabulary is unclassified, never a success', () => {
    // outgress's own vocabulary. A caller that needs the real code reads the
    // reply through rpcReply instead of letting rpc throw this at it.
    const r = rpcRefusal({ error: 'this Discord server is not connected to your Twitch channel', code: 'not_bound' });
    expect(r).toBeInstanceOf(RpcError);
    expect(r?.code).toBe('');
  });
});
