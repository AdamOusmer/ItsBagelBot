// In-memory, per-pod token bucket rate limiter for the console SSR backends.
//
// Deliberately NOT Valkey-backed: the write master lives in NA, so a
// per-request Valkey write from the EU node would spend the p99 budget on a
// transatlantic round-trip just to count requests. Per-node PreferClose routing
// already gives rough client→pod affinity, so a per-pod limit multiplied by the
// replica count is an acceptable global ceiling — this is an abuse brake, not
// an exact quota.
//
// Classic token bucket: each key holds up to `capacity` tokens (the burst) and
// refills continuously at `refillPerSec`. A request spends one token; an empty
// bucket rejects with the seconds until one token is available again.

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
