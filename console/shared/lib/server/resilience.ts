// Fault-isolation primitives shared by the console SSR backends.
//
// The SSR hot path fans out to several independent dependencies (Valkey, the
// projector, per-service RPC). A single slow or broken dependency must never
// take a page down or blow the p99 budget. Two tools cover that:
//
//   * withTimeout — bound any awaited dependency so a hung responder degrades
//     fast instead of hanging SSR to a gateway 500.
//   * CircuitBreaker — once a dependency is failing, stop calling it (fail fast
//     to the fallback) until it recovers, so one bad dependency does not burn
//     the timeout budget on every request.

/** Rejects with a TimeoutError if `promise` does not settle within `ms`. */
export class TimeoutError extends Error {}

export async function withTimeout<T>(promise: Promise<T>, ms: number, label = 'operation'): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new TimeoutError(`${label} timed out after ${ms}ms`)), ms);
  });
  try {
    return await Promise.race([promise, timeout]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

/** Thrown by CircuitBreaker.run when the circuit is open (dependency presumed down). */
export class CircuitOpenError extends Error {
  constructor(name: string) {
    super(`circuit "${name}" is open`);
  }
}

export interface CircuitBreakerOptions {
  /** Consecutive failures before the circuit trips open. Default 5. */
  failureThreshold?: number;
  /** How long the circuit stays open before a half-open trial. Default 5s. */
  resetMs?: number;
  /** For metrics/labels. */
  name?: string;
}

type State = 'closed' | 'open' | 'half-open';

/**
 * Per-dependency circuit breaker. Wrap each external dependency in its own
 * breaker so a fault in one does not affect calls to the others.
 *
 * closed     -> calls pass through; consecutive failures are counted.
 * open       -> calls fail fast with CircuitOpenError until resetMs elapses.
 * half-open  -> one trial call is allowed; success closes, failure re-opens.
 */
export class CircuitBreaker {
  private state: State = 'closed';
  private failures = 0;
  private openedAt = 0;
  private readonly threshold: number;
  private readonly resetMs: number;
  readonly name: string;

  constructor(opts: CircuitBreakerOptions = {}) {
    this.threshold = opts.failureThreshold ?? 5;
    this.resetMs = opts.resetMs ?? 5_000;
    this.name = opts.name ?? 'circuit';
  }

  /** True if a call would currently fail fast (open and not yet eligible for a trial). */
  get isOpen(): boolean {
    if (this.state !== 'open') return false;
    return Date.now() - this.openedAt < this.resetMs;
  }

  /**
   * Run `fn` under the breaker. Throws CircuitOpenError without calling `fn`
   * when the circuit is open. Callers that must degrade gracefully should catch
   * and fall back (see {@link run}'s typical use in the rpc wrappers).
   */
  async run<T>(fn: () => Promise<T>): Promise<T> {
    if (this.state === 'open') {
      if (Date.now() - this.openedAt < this.resetMs) throw new CircuitOpenError(this.name);
      this.state = 'half-open';
    }
    try {
      const out = await fn();
      this.onSuccess();
      return out;
    } catch (err) {
      this.onFailure();
      throw err;
    }
  }

  private onSuccess(): void {
    this.failures = 0;
    this.state = 'closed';
  }

  private onFailure(): void {
    this.failures++;
    if (this.state === 'half-open' || this.failures >= this.threshold) {
      this.state = 'open';
      this.openedAt = Date.now();
    }
  }
}
