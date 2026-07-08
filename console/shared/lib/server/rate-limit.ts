// Token bucket rate limiting for the console SSR backends.
//
// Two backends share the same bucket semantics (up to `capacity` tokens, a
// continuous refill of `refillPerSec`, one token per request):
//
//   * RateLimiter        — in-memory, per-pod. Zero dependencies, used directly
//                          in tests and as the fallback tier.
//   * ValkeyRateLimiter  — fleet-wide. The bucket lives in Valkey and is
//                          updated by one atomic Lua script, so every pod
//                          enforces the same global budget. Connects through
//                          Sentinel (when configured) so writes always reach
//                          the elected master across failovers.
//
// Fail-safe posture: the Valkey path runs under a circuit breaker and a
// per-op timeout, exactly like the valkey-store read tier. Any failure — no
// config, connection down, slow op, open circuit — degrades to the embedded
// per-pod limiter. Rate limiting must never take a page down; the worst
// failure mode is a looser (per-pod) limit for a few seconds.

import Redis from 'iovalkey';
import { getServerConfig, hasServerConfig } from './config';
import { CircuitBreaker, withTimeout } from './resilience';

export interface RateLimiterOptions {
  /** Burst size: requests allowed instantly from a cold/full bucket. */
  capacity: number;
  /** Sustained rate: tokens restored per second. */
  refillPerSec: number;
  /**
   * Memory ceiling on tracked keys. When exceeded, idle (fully refilled)
   * buckets are swept, then the oldest survivors are evicted. Default 50_000.
   */
  maxKeys?: number;
  /** Clock override for tests. Milliseconds, defaults to Date.now. */
  now?: () => number;
}

export interface RateDecision {
  allowed: boolean;
  /** Whole seconds the client should wait before retrying; 0 when allowed. */
  retryAfterSec: number;
}

interface Bucket {
  tokens: number;
  /** Last refill timestamp (ms). */
  at: number;
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

    // Drop idle buckets so long-gone clients do not accumulate forever. unref:
    // the sweeper must never hold the process open on shutdown.
    this.sweeper = setInterval(() => this.sweep(), SWEEP_INTERVAL_MS);
    this.sweeper.unref?.();
  }

  /** Spend one token for `key`. Never throws. */
  check(key: string): RateDecision {
    const now = this.now();
    let bucket = this.buckets.get(key);

    if (!bucket) {
      if (this.buckets.size >= this.maxKeys) this.evict();
      bucket = { tokens: this.capacity, at: now };
      this.buckets.set(key, bucket);
    } else {
      const elapsedSec = (now - bucket.at) / 1000;
      if (elapsedSec > 0) {
        bucket.tokens = Math.min(this.capacity, bucket.tokens + elapsedSec * this.refillPerSec);
        bucket.at = now;
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

  /** Tracked key count; exposed for tests and metrics. */
  get size(): number {
    return this.buckets.size;
  }

  /** Stop the background sweeper (tests / graceful teardown). */
  dispose(): void {
    clearInterval(this.sweeper);
    this.buckets.clear();
  }

  /** Remove buckets that have fully refilled — an idle key costs nothing to recreate. */
  private sweep(): void {
    const now = this.now();
    for (const [key, bucket] of this.buckets) {
      const tokens = bucket.tokens + ((now - bucket.at) / 1000) * this.refillPerSec;
      if (tokens >= this.capacity) this.buckets.delete(key);
    }
  }

  /**
   * Over the key ceiling: sweep idle buckets first; if that is not enough,
   * evict the oldest entries (Map preserves insertion order) so hot keys —
   * the ones being limited — survive.
   */
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

// ---------------------------------------------------------------------------
// Valkey-backed fleet-wide limiter.

// The whole bucket update is one atomic script, so concurrent requests across
// pods never interleave between the read and the write. Time comes from the
// server (TIME) rather than the pods, so bucket refill is immune to pod clock
// skew. Returns {allowed, retryAfterSec}.
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

// One connection + one breaker for every limiter tier: it is a single
// dependency, and a second tier probing a dead master buys nothing but
// another timeout.
let writeClient: RateLimitClient | null = null;
let writeDisabled = false;
let writeBreaker = newWriteBreaker();

function newWriteBreaker(): CircuitBreaker {
  return new CircuitBreaker({ name: 'valkey-ratelimit', failureThreshold: 3, resetMs: 5_000 });
}

function getWriteClient(): RateLimitClient | null {
  if (writeDisabled) return null;
  if (writeClient) return writeClient;
  // Config is registered by the init hook; tolerate its absence (unit tests,
  // misordered boot) by degrading to the per-pod fallback instead of throwing.
  const cfg = hasServerConfig() ? getServerConfig().valkey : undefined;
  if (!cfg) {
    writeDisabled = true;
    return null;
  }

  let client: Redis;
  if (cfg.sentinelAddr) {
    const [host, portStr] = cfg.sentinelAddr.split(':');
    client = new Redis({
      sentinels: [{ host: host || '127.0.0.1', port: portStr ? Number(portStr) : 26379 }],
      // `||` not `??`: an empty VALKEY_MASTER_SET (unset in Doppler comes
      // through as "") must fall back to the sentinel's monitored name, not be
      // used verbatim — a blank master name never resolves, so every write
      // (rate-limit AND single-use claimOnce) would silently time out.
      name: cfg.sentinelMaster || 'myprimary',
      password: cfg.password || undefined,
      // Fail fast, never queue: an unreachable master must degrade to the
      // per-pod fallback immediately, not grow an unbounded command queue.
      enableOfflineQueue: false,
      maxRetriesPerRequest: 1,
      connectTimeout: 1000,
      retryStrategy: (times) => Math.min(times * 200, 2000),
      sentinelRetryStrategy: (times: number) => Math.min(times * 200, 2000)
    });
  } else {
    const [host, portStr] = cfg.addr.split(':');
    client = new Redis({
      host: host || '127.0.0.1',
      port: portStr ? Number(portStr) : 6379,
      password: cfg.password || undefined,
      enableOfflineQueue: false,
      maxRetriesPerRequest: 1,
      connectTimeout: 1000,
      retryStrategy: (times) => Math.min(times * 200, 2000)
    });
  }

  // Swallow connection errors; ops fail fast and the limiter falls back local.
  client.on('error', () => {});
  // defineCommand handles EVALSHA with automatic EVAL fallback on script-cache
  // misses (fresh master after a failover, restarted instance).
  client.defineCommand('rateLimit', { numberOfKeys: 1, lua: TOKEN_BUCKET_LUA });
  writeClient = client as RateLimitClient;
  return writeClient;
}

/**
 * Pre-connect the write client at boot so the first request does not pay the
 * dial. Best-effort, no-op when Valkey is unconfigured. Call from the init hook.
 */
export function warmRateLimiter(): void {
  getWriteClient();
}

/**
 * Probe the write path (PING under the breaker + timeout), connecting it as a
 * side effect. Returns true when the backend is reachable OR unconfigured —
 * the fleet-wide tier is optional (per-pod fallback), never a readiness
 * blocker. Wire into /readyz next to the valkey-store probe so a rotated-in
 * pod's write client is hot within one probe interval.
 */
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

/**
 * Claim a single-use id (SET NX EX on the Sentinel master), sharing the
 * rate limiter's write client + breaker: it is the same fleet-wide Valkey
 * write path and a second connection buys nothing. Used to make signed
 * one-shot tokens (e.g. admin "view as" links) non-replayable.
 *
 *   'claimed'      — first redemption; proceed.
 *   'replayed'     — the id was redeemed before; reject.
 *   'unconfigured' — no Valkey configured (dev/tests); caller decides.
 *   'unavailable'  — backend configured but unreachable; caller decides.
 */
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

/**
 * Drop the cached write client and its disabled latch so a test can re-run
 * client resolution against fresh config. Test-only; never call in app code.
 */
export function resetRateLimiterBackendForTests(): void {
  writeClient?.disconnect();
  writeClient = null;
  writeDisabled = false;
  writeBreaker = newWriteBreaker();
}

export interface ValkeyRateLimiterOptions {
  /** Key namespace for this tier, e.g. "auth" → keys "rl:auth:<key>". */
  name: string;
  capacity: number;
  refillPerSec: number;
  /** Fallback tuning override; defaults to the same capacity/refill. */
  fallback?: RateLimiterOptions;
}

/**
 * Fleet-wide token bucket in Valkey with a per-pod in-memory fallback. check()
 * never throws and never exceeds ~OP_TIMEOUT_MS added latency.
 */
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
      } catch {
        // Open circuit, timeout, READONLY replica, connection down — fall
        // through to the per-pod bucket below.
      }
    }
    return this.fallback.check(key);
  }

  /** Release the fallback sweeper (tests / teardown). */
  dispose(): void {
    this.fallback.dispose();
  }
}

/**
 * Resolve the client IP for rate-limit keying behind the cloudflared → traefik
 * chain. Cf-Connecting-Ip is set by Cloudflare itself and cannot be spoofed
 * through the tunnel, so it wins; X-Forwarded-For's first hop covers direct
 * tailnet/edge access; `fallback` is the socket address (getClientAddress).
 */
export function clientIp(headers: Headers, fallback: () => string): string {
  const cf = headers.get('cf-connecting-ip');
  if (cf) return cf.trim();
  const xff = headers.get('x-forwarded-for');
  if (xff) {
    const first = xff.split(',')[0]?.trim();
    if (first) return first;
  }
  try {
    return fallback();
  } catch {
    // adapter-node can throw when the socket is already gone; one shared
    // bucket for unattributable requests is better than throwing into SSR.
    return 'unknown';
  }
}
