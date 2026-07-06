import { describe, expect, test } from 'bun:test';
import { RateLimiter, clientIp } from './rate-limit';

/** Limiter on a manual clock so refill is deterministic. */
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

    advance(1000); // one token back
    expect(limiter.check('k').allowed).toBe(true);
    expect(limiter.check('k').allowed).toBe(false);

    advance(60_000); // caps at capacity, not 60 tokens
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
    // 1 token per 10s: an empty bucket says wait 10s.
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
    limiter.check('d'); // over ceiling: 'a' evicted
    expect(limiter.size).toBe(3);
    // 'a' was forgotten, so it gets a fresh burst; 'd' is still tracked/empty.
    expect(limiter.check('a').allowed).toBe(true);
    expect(limiter.check('d').allowed).toBe(false);
    limiter.dispose();
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
