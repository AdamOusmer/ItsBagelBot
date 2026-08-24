// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { NoRespondersError, RequestError, TimeoutError } from '@nats-io/transport-node';
import { requestLocalFirst, rpcSubjectsForNode } from './nats-rpc-locality';

describe('RPC node locality', () => {
  test('builds a restricted node-qualified subject', () => {
    expect(rpcSubjectsForNode('bagel.rpc.users.get', 'node2')).toEqual([
      'bagel.rpc.users.get.node.node2',
      'bagel.rpc.users.get'
    ]);
  });

  test('keeps generic-only routing for missing or unsafe node tokens', () => {
    expect(rpcSubjectsForNode('rpc.get', undefined)).toEqual(['rpc.get']);
    expect(rpcSubjectsForNode('rpc.get', 'zone.node2')).toEqual(['rpc.get']);
    expect(rpcSubjectsForNode('rpc.get', 'node*')).toEqual(['rpc.get']);
  });

  test('falls back to the generic subject only for no responders', async () => {
    const called: string[] = [];
    const result = await requestLocalFirst(['rpc.get.node.node2', 'rpc.get'], async (subject) => {
      called.push(subject);
      if (subject.endsWith('.node.node2')) {
        throw new RequestError(`no responders for '${subject}'`, {
          cause: new NoRespondersError(subject)
        });
      }
      return 'generic reply';
    });
    expect(result).toBe('generic reply');
    expect(called).toEqual(['rpc.get.node.node2', 'rpc.get']);
  });

  test('does not replay an ambiguous timeout', async () => {
    const called: string[] = [];
    await expect(
      requestLocalFirst(['rpc.write.node.node2', 'rpc.write'], async (subject) => {
        called.push(subject);
        throw new TimeoutError();
      })
    ).rejects.toBeInstanceOf(TimeoutError);
    expect(called).toEqual(['rpc.write.node.node2']);
  });
});
