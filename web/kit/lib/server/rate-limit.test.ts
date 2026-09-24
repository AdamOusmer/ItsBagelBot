// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  RateLimiter,
  ValkeyRateLimiter,
  clientIp,
  rateLimiterReady,
  resetRateLimiterBackendForTests
} from './rate-limit';
import { registerServerConfig } from './config';

function make(opts: { capacity: number; refillPerSec: number; maxKeys?: number }) {
  let t = 0;
  const limiter = new RateLimiter({ ...opts, now: () => t });
  return { limiter, advance: (ms: number) => (t += ms) };
}

describe('RateLimiter', () => {
  test('allows the full burst then rejects', () => {
    const { limiter } = make({ capacity: 3, refillPerSec: 1 });
    expect(limiter.check('k').allowed).toBe(true);
    expect(limiter.check('k').allowed).toBe(true);
    expect(limiter.check('k').allowed).toBe(true);
    const denied = limiter.check('k');
    expect(denied.allowed).toBe(false);
    expect(denied.retryAfterSec).toBeGreaterThanOrEqual(1);
    limiter.dispose();
  });

  test('refills over time up to capacity', () => {
    const { limiter, advance } = make({ capacity: 2, refillPerSec: 1 });
    limiter.check('k');
    limiter.check('k');
    expect(limiter.check('k').allowed).toBe(false);

    advance(1000);
    expect(limiter.check('k').allowed).toBe(true);
    expect(limiter.check('k').allowed).toBe(false);

    advance(60_000);
    expect(limiter.check('k').allowed).toBe(true);
    expect(limiter.check('k').allowed).toBe(true);
    expect(limiter.check('k').allowed).toBe(false);
    limiter.dispose();
  });

  test('keys are independent', () => {
    const { limiter } = make({ capacity: 1, refillPerSec: 1 });
    expect(limiter.check('a').allowed).toBe(true);
    expect(limiter.check('a').allowed).toBe(false);
    expect(limiter.check('b').allowed).toBe(true);
    limiter.dispose();
  });

  test('retryAfterSec reflects the refill rate', () => {
    const { limiter } = make({ capacity: 1, refillPerSec: 0.1 });
    limiter.check('k');
    expect(limiter.check('k').retryAfterSec).toBe(10);
    limiter.dispose();
  });

  test('evicts oldest keys at the ceiling, keeps serving', () => {
    const { limiter } = make({ capacity: 1, refillPerSec: 0, maxKeys: 3 });
    limiter.check('a');
    limiter.check('b');
    limiter.check('c');
    limiter.check('d');
    expect(limiter.size).toBe(3);
    expect(limiter.check('a').allowed).toBe(true);
    expect(limiter.check('d').allowed).toBe(false);
    limiter.dispose();
  });
});

describe('ValkeyRateLimiter', () => {
  test('degrades to the per-pod bucket when Valkey is unconfigured', async () => {
    resetRateLimiterBackendForTests();
    const limiter = new ValkeyRateLimiter({ name: 'test', capacity: 2, refillPerSec: 1 });
    expect((await limiter.check('k')).allowed).toBe(true);
    expect((await limiter.check('k')).allowed).toBe(true);
    const denied = await limiter.check('k');
    expect(denied.allowed).toBe(false);
    expect(denied.retryAfterSec).toBeGreaterThanOrEqual(1);
    limiter.dispose();
  });

  test('degrades to the per-pod bucket when Valkey is unreachable', async () => {
    resetRateLimiterBackendForTests();
    registerServerConfig({
      valkey: { addr: '127.0.0.1:1' },
      cacheInvalidationPrefix: 'test'
    });
    const limiter = new ValkeyRateLimiter({ name: 'down', capacity: 1, refillPerSec: 0.001 });
    expect((await limiter.check('k')).allowed).toBe(true);
    expect((await limiter.check('k')).allowed).toBe(false);
    limiter.dispose();
    resetRateLimiterBackendForTests();
  });

  const integrationAddr = process.env.RATE_LIMIT_TEST_VALKEY;
  test.if(!!integrationAddr)('enforces one shared budget across limiter instances', async () => {
    resetRateLimiterBackendForTests();
    registerServerConfig({ valkey: { addr: integrationAddr! }, cacheInvalidationPrefix: 'test' });

    const deadline = Date.now() + 2000;
    while (!(await rateLimiterReady())) {
      if (Date.now() > deadline) throw new Error('valkey backend never became ready');
      await new Promise((r) => setTimeout(r, 50));
    }

    const key = `it-${Date.now()}`;
    const podA = new ValkeyRateLimiter({ name: 'itest', capacity: 3, refillPerSec: 1 });
    const podB = new ValkeyRateLimiter({ name: 'itest', capacity: 3, refillPerSec: 1 });

    expect((await podA.check(key)).allowed).toBe(true);
    expect((await podB.check(key)).allowed).toBe(true);
    expect((await podA.check(key)).allowed).toBe(true);

    const denied = await podB.check(key);
    expect(denied.allowed).toBe(false);
    expect(denied.retryAfterSec).toBeGreaterThanOrEqual(1);

    await new Promise((r) => setTimeout(r, 1100));
    expect((await podB.check(key)).allowed).toBe(true);
    expect((await podA.check(key)).allowed).toBe(false);

    podA.dispose();
    podB.dispose();
    resetRateLimiterBackendForTests();
  });
});

describe('clientIp', () => {
  const fallback = () => '10.0.0.1';

  test('prefers Cf-Connecting-Ip', () => {
    const h = new Headers({ 'cf-connecting-ip': '1.2.3.4', 'x-forwarded-for': '5.6.7.8' });
    expect(clientIp(h, fallback)).toBe('1.2.3.4');
  });

  test('falls back to first X-Forwarded-For hop', () => {
    const h = new Headers({ 'x-forwarded-for': ' 5.6.7.8 , 9.9.9.9' });
    expect(clientIp(h, fallback)).toBe('5.6.7.8');
  });

  test('uses the socket address when no proxy headers exist', () => {
    expect(clientIp(new Headers(), fallback)).toBe('10.0.0.1');
  });

  test('never throws when the fallback does', () => {
    expect(
      clientIp(new Headers(), () => {
        throw new Error('socket gone');
      })
    ).toBe('unknown');
  });
});
