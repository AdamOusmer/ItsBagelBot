// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type Redis from 'iovalkey';
import { masterClient, resetMasterClientForTests } from './valkey-master';
import { CircuitBreaker, withTimeout } from './resilience';

export interface RateLimiterOptions {
  capacity: number;
  refillPerSec: number;
  maxKeys?: number;
  now?: () => number;
}

export interface RateDecision {
  allowed: boolean;
  retryAfterSec: number;
}

interface Bucket {
  tokens: number;
  refilledAtMs: number;
}

const SWEEP_INTERVAL_MS = 60_000;
const DEFAULT_MAX_KEYS = 50_000;

export class RateLimiter {
  private readonly buckets = new Map<string, Bucket>();
  private readonly capacity: number;
  private readonly refillPerSec: number;
  private readonly maxKeys: number;
  private readonly now: () => number;
  private readonly sweeper: ReturnType<typeof setInterval>;

  constructor(opts: RateLimiterOptions) {
    this.capacity = opts.capacity;
    this.refillPerSec = opts.refillPerSec;
    this.maxKeys = opts.maxKeys ?? DEFAULT_MAX_KEYS;
    this.now = opts.now ?? Date.now;

    this.sweeper = setInterval(() => this.sweep(), SWEEP_INTERVAL_MS);
    this.sweeper.unref?.();
  }

  check(key: string): RateDecision {
    const now = this.now();
    let bucket = this.buckets.get(key);

    if (!bucket) {
      if (this.buckets.size >= this.maxKeys) this.evict();
      bucket = { tokens: this.capacity, refilledAtMs: now };
      this.buckets.set(key, bucket);
    } else {
      const elapsedSec = (now - bucket.refilledAtMs) / 1000;
      if (elapsedSec > 0) {
        bucket.tokens = Math.min(this.capacity, bucket.tokens + elapsedSec * this.refillPerSec);
        bucket.refilledAtMs = now;
      }
    }

    if (bucket.tokens >= 1) {
      bucket.tokens -= 1;
      return { allowed: true, retryAfterSec: 0 };
    }

    return {
      allowed: false,
      retryAfterSec: Math.max(1, Math.ceil((1 - bucket.tokens) / this.refillPerSec))
    };
  }

  get size(): number {
    return this.buckets.size;
  }

  dispose(): void {
    clearInterval(this.sweeper);
    this.buckets.clear();
  }

  private sweep(): void {
    const now = this.now();
    for (const [key, bucket] of this.buckets) {
      const tokens = bucket.tokens + ((now - bucket.refilledAtMs) / 1000) * this.refillPerSec;
      if (tokens >= this.capacity) this.buckets.delete(key);
    }
  }

  private evict(): void {
    this.sweep();
    let excess = this.buckets.size - this.maxKeys + 1;
    if (excess <= 0) return;
    for (const key of this.buckets.keys()) {
      this.buckets.delete(key);
      if (--excess <= 0) return;
    }
  }
}

const TOKEN_BUCKET_LUA = `
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local state = redis.call('HMGET', KEYS[1], 't', 'ts')
local tokens = tonumber(state[1])
local ts = tonumber(state[2])
if tokens == nil or ts == nil then
  tokens = capacity
  ts = now
end
tokens = math.min(capacity, tokens + math.max(0, now - ts) / 1000 * refill)
local allowed = 0
local retry = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry = math.ceil((1 - tokens) / refill)
  if retry < 1 then retry = 1 end
end
redis.call('HSET', KEYS[1], 't', tostring(tokens), 'ts', now)
redis.call('PEXPIRE', KEYS[1], math.ceil(capacity / refill * 1000) + 60000)
return {allowed, retry}
`;

interface RateLimitClient extends Redis {
  rateLimit(key: string, capacity: number, refillPerSec: number): Promise<[number, number]>;
}

const OP_TIMEOUT_MS = 150;

let writeBreaker = newWriteBreaker();
const scripted = new WeakSet<Redis>();

function newWriteBreaker(): CircuitBreaker {
  return new CircuitBreaker({ name: 'valkey-ratelimit', failureThreshold: 3, resetMs: 5_000 });
}

function getWriteClient(): RateLimitClient | null {
  const client = masterClient();
  if (!client) return null;
  if (!scripted.has(client)) {
    client.defineCommand('rateLimit', { numberOfKeys: 1, lua: TOKEN_BUCKET_LUA });
    scripted.add(client);
  }
  return client as RateLimitClient;
}

export function rateLimitClientForTests(): Redis | null {
  return masterClient();
}

export function warmRateLimiter(): void {
  getWriteClient();
}

export async function rateLimiterReady(): Promise<boolean> {
  const client = getWriteClient();
  if (!client) return true;
  try {
    await writeBreaker.run(() => withTimeout(client.ping(), OP_TIMEOUT_MS, 'valkey rate-limit'));
    return true;
  } catch {
    return false;
  }
}

export type ClaimResult = 'claimed' | 'replayed' | 'unconfigured' | 'unavailable';

export async function claimOnce(key: string, ttlSec: number): Promise<ClaimResult> {
  const client = getWriteClient();
  if (!client) return 'unconfigured';
  try {
    const r = await writeBreaker.run(() =>
      withTimeout(client.set(key, '1', 'EX', ttlSec, 'NX'), OP_TIMEOUT_MS, 'valkey claim-once')
    );
    return r === 'OK' ? 'claimed' : 'replayed';
  } catch {
    return 'unavailable';
  }
}

export function resetRateLimiterBackendForTests(): void {
  resetMasterClientForTests();
  writeBreaker = newWriteBreaker();
}

export interface ValkeyRateLimiterOptions {
  name: string;
  capacity: number;
  refillPerSec: number;
  fallback?: RateLimiterOptions;
}

export class ValkeyRateLimiter {
  private readonly prefix: string;
  private readonly capacity: number;
  private readonly refillPerSec: number;
  private readonly fallback: RateLimiter;

  constructor(opts: ValkeyRateLimiterOptions) {
    this.prefix = `rl:${opts.name}:`;
    this.capacity = opts.capacity;
    this.refillPerSec = opts.refillPerSec;
    this.fallback = new RateLimiter(
      opts.fallback ?? { capacity: opts.capacity, refillPerSec: opts.refillPerSec }
    );
  }

  async check(key: string): Promise<RateDecision> {
    const client = getWriteClient();
    if (client) {
      try {
        const [allowed, retry] = await writeBreaker.run(() =>
          withTimeout(
            client.rateLimit(this.prefix + key, this.capacity, this.refillPerSec),
            OP_TIMEOUT_MS,
            'valkey rate-limit'
          )
        );
        return allowed === 1
          ? { allowed: true, retryAfterSec: 0 }
          : { allowed: false, retryAfterSec: retry };
      } catch {}
    }
    return this.fallback.check(key);
  }

  dispose(): void {
    this.fallback.dispose();
  }
}
