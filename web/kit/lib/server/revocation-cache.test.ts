// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { RevocationCache, type RevocationRead } from './revocation-cache';

function setup() {
  let t = 0;
  const cache = new RevocationCache(() => t);
  const calls = { n: 0 };
  const load = async (): Promise<RevocationRead> => {
    calls.n++;
    return [null, null];
  };
  return { cache, calls, load, advance: (ms: number) => (t += ms) };
}

describe('RevocationCache', () => {
  test('a read is reused for under two seconds, then read again', async () => {
    const { cache, calls, load, advance } = setup();
    const keys = { sid: 's1', userId: 'u1' };
    await cache.read(keys, load);
    advance(1_999);
    await cache.read(keys, load);
    expect(calls.n).toBe(1);
    advance(1);
    await cache.read(keys, load);
    expect(calls.n).toBe(2);
  });

  test('a local revoke holds for ten seconds, then defers to Valkey', () => {
    const { cache, advance } = setup();
    cache.remember({ key: 'sid:s1', epoch: Number.POSITIVE_INFINITY });
    advance(9_999);
    expect(cache.revokedHere({ sid: 's1', userId: 'u1', iat: 1000 })).toBe(true);
    advance(1);
    expect(cache.revokedHere({ sid: 's1', userId: 'u1', iat: 1000 })).toBe(false);
  });
});
