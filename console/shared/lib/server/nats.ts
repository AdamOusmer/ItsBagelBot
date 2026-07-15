// Server-only NATS RPC client. One lazily-dialed, process-wide connection
// reused across requests (connection setup is the expensive part; a warm conn
// keeps request/reply in the low-ms range, which is what the p99 budget needs).
import newrelic from 'newrelic';
import {
  connect,
  JSONCodec,
  type ConnectionOptions,
  type NatsConnection,
  type JetStreamClient,
  type JetStreamManager,
  type JetStreamManagerOptions
} from 'nats';

const jc = JSONCodec();

// Collapse numeric/id path tokens so per-subject RPC segments stay low-cardinality
// in New Relic (e.g. `...status.12345` -> `...status.*`).
function rpcSegment(subject: string): string {
  return subject
    .split('.')
    .map((t) => (/^\d+$/.test(t) ? '*' : t))
    .join('.');
}

// The console holds two connections on two accounts (per-account isolation):
//
//   * 'rpc'  — the per-service RPC account (NATS_RPC_USER/PASSWORD): request/reply
//     (bagel.rpc.*) and the cache-invalidation subscription (bagel.cache.*).
//   * 'bus'  — the shared BUS account (NATS_USER/PASSWORD): the JetStream lane
//     view (admin) and the outgress-system stream-feed publish (twitch.*).
//
// RPC prefers the strict same-node leaf and falls back to the hub. BUS connects
// directly to the hub so JetStream never pays a leaf hop.
type Role = 'rpc' | 'bus';

interface Pool {
  conn: NatsConnection | null;
  dialing: Promise<NatsConnection> | null;
}

const pools: Record<Role, Pool> = {
  rpc: { conn: null, dialing: null },
  bus: { conn: null, dialing: null }
};

const failbackEnabled = new WeakSet<NatsConnection>();
let failbackQueue: Promise<void> = Promise.resolve();

function positiveNumber(value: string | undefined, fallback: number): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

export async function localLeafReady(healthURL: string, timeoutMs: number): Promise<boolean> {
  try {
    const response = await fetch(healthURL, {
      signal: AbortSignal.timeout(timeoutMs),
      cache: 'no-store'
    });
    await response.body?.cancel();
    return response.ok;
  } catch {
    return false;
  }
}

/**
 * Re-home a connection only after it is known to be remote and the strict
 * same-node probe has succeeded repeatedly. Reconnects are serialized across
 * the process and initially jittered to avoid a fleet-wide recovery stampede.
 */
export function enableLeafFailback(nc: NatsConnection): void {
  const nodeName = process.env.NODE_NAME;
  if (!nodeName || failbackEnabled.has(nc)) return;
  failbackEnabled.add(nc);

  const intervalMs = positiveNumber(process.env.NATS_FAILBACK_INTERVAL_MS, 30_000);
  const required = positiveNumber(process.env.NATS_FAILBACK_SUCCESSES, 3);
  const timeoutMs = positiveNumber(process.env.NATS_FAILBACK_PROBE_TIMEOUT_MS, 1_000);
  const healthURL =
    process.env.NATS_LOCAL_LEAF_HEALTH_URL ?? 'http://nats-leaf-local:8222/healthz';
  let consecutive = 0;
  let running = false;

  const check = async () => {
    if (running || nc.isClosed()) return;
    running = true;
    try {
      const serverName = nc.info?.server_name ?? '';
      if (serverName.startsWith(`${nodeName}--`)) {
        consecutive = 0;
        return;
      }
      if (!(await localLeafReady(healthURL, timeoutMs))) {
        consecutive = 0;
        return;
      }
      consecutive++;
      if (consecutive < required) return;
      consecutive = 0;
      // The queued task must never reject: a rejecting reconnect() would leave
      // the queue tail rejected and stall every later failback until the next
      // .catch() re-arms it. Swallow inside the task; the probe cycle retries.
      failbackQueue = failbackQueue.then(async () => {
        try {
          if (!nc.isClosed() && !nc.info?.server_name?.startsWith(`${nodeName}--`)) {
            await nc.reconnect();
          }
        } catch {
          /* reconnect failed; next probe cycle retries */
        }
      });
      await failbackQueue;
    } catch {
      consecutive = 0;
    } finally {
      running = false;
    }
  };

  const initial = setTimeout(() => {
    void check();
    const timer = setInterval(() => void check(), intervalMs);
    timer.unref();
  }, Math.random() * intervalMs);
  initial.unref();
}

function fallbackServer(override: string | undefined): string {
  return override ?? `nats://${process.env.NATS_HOST ?? '127.0.0.1'}:${process.env.NATS_PORT ?? '4222'}`;
}

// Ordered RPC pool. Legacy NATS_LEAF_URL values are intentionally ignored so
// a stale secret cannot override the strict local-only NATS_RPC_URL.
function rpcServerList(override: string | undefined): string[] {
  return [fallbackServer(override)];
}

function busServerList(override: string | undefined): string[] {
  return [process.env.NATS_HUB_URL ?? fallbackServer(override)];
}

function options(role: Role): ConnectionOptions {
  const isRpc = role === 'rpc';
  const opts: ConnectionOptions = {
    servers: isRpc
      ? rpcServerList(process.env.NATS_RPC_URL)
      : busServerList(process.env.NATS_URL),
    // RPC stays on the leaf tier; the Service handles cross-node leaf failover.
    noRandomize: true,
    name: `${process.env.NATS_CLIENT_NAME ?? 'console'}-${role}`,
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
    // Broker auth/config and Doppler-driven app restarts can briefly land out
    // of order during a rollout. Keep reconnecting through that window instead
    // of permanently closing after the client's default two auth failures.
    ignoreAuthErrorAbort: true,
    pingInterval: 20_000,
    // Bound the initial dial so a cold/unreachable NATS fails fast (and re-dials
    // on the next request) instead of hanging SSR to the 20s default and surfacing
    // a gateway "server connection error" the user has to refresh past.
    timeout: 3_000
  };
  const user = isRpc ? (process.env.NATS_RPC_USER ?? process.env.NATS_USER) : process.env.NATS_USER;
  const pass = isRpc
    ? (process.env.NATS_RPC_PASSWORD ?? process.env.NATS_PASSWORD)
    : process.env.NATS_PASSWORD;
  if (user) opts.user = user;
  if (pass) opts.pass = pass;
  if (process.env.NATS_TOKEN) opts.token = process.env.NATS_TOKEN;
  // Verify the NATS server's TLS cert against the fleet CA now that NATS is out of
  // the Linkerd mesh (NATS_CA_PEM = the trust-manager fleet-ca ConfigMap). Setting
  // tls upgrades the connection; server-auth only, auth stays user/password. No CA
  // (local dev against a plaintext server) keeps the connection plaintext.
  const caPem = process.env.NATS_CA_PEM;
  if (caPem) opts.tls = { ca: caPem };
  return opts;
}

async function get(role: Role): Promise<NatsConnection> {
  const pool = pools[role];
  if (pool.conn && !pool.conn.isClosed()) return pool.conn;
  if (pool.dialing) return pool.dialing;
  // Clear `dialing` in finally, not in the success handler: if connect() rejects
  // (NATS down at dial time) the success handler never runs, so leaving it set
  // would pin a rejected promise here and fail every later request until the
  // process restarts. finally lets the next get() re-dial.
  pool.dialing = connect(options(role))
    .then((c) => {
      // Tolerate a close that raced the dial (e.g. shutdown mid-connect): treat
      // it as a failed dial so the next get() re-dials instead of pinning a
      // dead connection in the pool.
      if (c.isClosed()) throw new Error('nats connection closed during dial');
      pool.conn = c;
      // JetStream wrappers retain the connection they were created from. If a
      // fully closed BUS connection is replaced (as opposed to reconnecting in
      // place), force lane/KV callers to derive fresh wrappers from this one.
      if (role === 'bus') {
        jsClient = null;
        jsManager = null;
      }
      if (role === 'rpc') enableLeafFailback(c);
      return c;
    })
    .finally(() => {
      pool.dialing = null;
    });
  return pool.dialing;
}

let jsClient: JetStreamClient | null = null;
let jsManager: JetStreamManager | null = null;

// BUS connects directly to the hub, so its JetStream API is the standard
// $JS.API prefix. Using domain:'hub' here makes the client generate a domain
// alias that the server remaps before account permission checks; the admin
// account then sees a denied $JS.API request and every lane view times out.
// Skip the extra enablement probe and return a fresh options object because the
// NATS client normalizes/mutates JetStream options internally.
export function hubJetStreamOptions(): JetStreamManagerOptions {
  return { apiPrefix: '$JS.API', checkAPI: false };
}

export async function js(): Promise<JetStreamClient> {
  const nc = await get('bus');
  if (!jsClient) jsClient = nc.jetstream(hubJetStreamOptions());
  return jsClient;
}

export async function jsm(): Promise<JetStreamManager> {
  const nc = await get('bus');
  if (!jsManager) jsManager = await nc.jetstreamManager(hubJetStreamOptions());
  return jsManager;
}

/**
 * Pre-dial the connection at server boot so the first user request hits a warm
 * conn instead of paying the cold dial on the hot path (the cause of the
 * intermittent "server connection error, refresh fixes it"). Best-effort: a
 * failed warm-up just leaves the next get() to re-dial.
 */
export function warm(): void {
  get('rpc').catch(() => {});
  get('bus').catch(() => {});
}

async function within<T>(promise: Promise<T>, timeoutMs: number): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error('timeout')), timeoutMs);
  });
  try {
    return await Promise.race([promise, timeout]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

export async function ready(timeoutMs = 750): Promise<boolean> {
  try {
    // The RPC connection is the SSR hot path; readiness tracks it.
    const nc = await within(get('rpc'), timeoutMs);
    await within(nc.flush(), timeoutMs);
    return !nc.isClosed();
  } catch {
    return false;
  }
}

export class RpcError extends Error {}

/**
 * Core NATS request/reply with a JSON body. Rejects on transport failure, on a
 * missing responder, or when the reply carries an `error` field. `timeoutMs`
 * defaults to 5s to match the Go callers.
 */
export async function rpc<T>(subject: string, payload: unknown = {}, timeoutMs = 5000): Promise<T> {
  // Time each request/reply as its own New Relic segment so the SSR transaction
  // breakdown shows which RPC subject dominates a slow page. Safe no-op (runs the
  // handler directly) when no agent/transaction is active.
  return newrelic.startSegment(`NATS/request/${rpcSegment(subject)}`, true, async () => {
    const nc = await get('rpc');
    const msg = await nc.request(subject, jc.encode(payload), { timeout: timeoutMs });
    const reply = jc.decode(msg.data) as T & { error?: string };
    if (reply && typeof reply === 'object' && reply.error) throw new RpcError(reply.error);
    return reply as T;
  });
}

/**
 * Fire-and-forget JSON publish (e.g. the outgress system lane). JetStream
 * streams capture core publishes to their subjects, so this enqueues the job
 * without needing a JetStream client.
 *
 * Routed by subject: stream-feed subjects (twitch.*) are captured by BUS-account
 * JetStream streams and must go on the bus connection; everything else stays on
 * the per-service RPC account.
 */
export async function publish(subject: string, payload: unknown = {}): Promise<void> {
  return newrelic.startSegment(`NATS/publish/${rpcSegment(subject)}`, true, async () => {
    // Stream-feed subjects (twitch.* / data.*) are captured by BUS-account
    // JetStream streams and must use the bus connection; RPC + cache stay on rpc.
    const role: Role = subject.startsWith('twitch.') || subject.startsWith('data.') ? 'bus' : 'rpc';
    const nc = await get(role);
    nc.publish(subject, jc.encode(payload));
    await nc.flush();
  });
}

export async function closeNats(): Promise<void> {
  for (const pool of Object.values(pools)) {
    if (pool.conn && !pool.conn.isClosed()) await pool.conn.drain();
    pool.conn = null;
  }
  jsClient = null;
  jsManager = null;
}

/**
 * Subscribe to a core NATS subject and call onMsg for every message. No queue
 * group — every replica receives every message, which is what the cache
 * invalidation bus needs (each process owns its own in-process cache).
 *
 * The callback receives both the message subject and raw data. For wildcard
 * subscriptions (e.g. `prefix.>`) the subject carries the full matched subject
 * of each individual message, letting callers derive scope from the last token.
 *
 * Fire-and-forget: resilient to dial failure (next get() re-dials), and
 * iterator errors just terminate the async loop silently rather than crashing
 * the server. Callers should call this once at boot (e.g. from hooks.server.ts)
 * and never await it.
 */
export function subscribe(subject: string, onMsg: (subject: string, data: Uint8Array) => void): void {
  // Cache-invalidation (bagel.cache.*) rides the per-service RPC account.
  get('rpc')
    .then((nc) => {
      const sub = nc.subscribe(subject);
      (async () => {
        try {
          for await (const m of sub) onMsg(m.subject, m.data);
        } catch {
          // Iterator closed (connection dropped / process shutting down) — ignore.
        }
      })();
    })
    .catch(() => {
      // Dial failed at subscribe time; the next request will re-dial via get().
    });
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    const t = setTimeout(resolve, ms);
    t.unref();
  });
}

function backoffMs(attempt: number): number {
  const base = Math.min(500 * 2 ** Math.min(attempt, 6), 30_000);
  return base / 2 + Math.random() * (base / 2); // jitter: [base/2, base)
}

/**
 * Durable core subscription for the cache-invalidation bus. Unlike subscribe(),
 * this NEVER gives up:
 *
 *   * a failed dial (NATS down at boot) is retried forever with exponential
 *     backoff + jitter — previously a boot-time outage silently killed the
 *     invalidation bus for the process lifetime, leaving replicas to decay by
 *     TTL alone;
 *   * if the message iterator terminates (connection closed/errored), the loop
 *     re-dials and resubscribes;
 *   * `onGap` fires after every window in which messages may have been missed:
 *     each client-level reconnect, every resubscribe after an iterator death,
 *     and an initial subscribe that only succeeded after failed attempts.
 *     Callers use it to flush their cache — a missed invalidation must not
 *     leave poisoned long-TTL entries.
 *
 * No queue group — every replica receives every message (each owns its own
 * in-process cache). onMsg exceptions are swallowed per message.
 */
export function subscribeDurable(
  subject: string,
  onMsg: (subject: string, data: Uint8Array) => void,
  onGap?: () => void
): void {
  const gap = () => {
    try {
      onGap?.();
    } catch {
      /* gap handler must not kill the loop */
    }
  };

  void (async () => {
    let attempt = 0;
    for (;;) {
      let nc: NatsConnection;
      try {
        nc = await get('rpc');
      } catch {
        attempt++;
        newrelic.recordMetric('Custom/NatsBus/dial_retry', 1);
        await sleep(backoffMs(attempt));
        continue;
      }

      // Messages published while we were not subscribed were lost: anything
      // after a failed first dial or a dead iterator is a gap.
      if (attempt > 0) {
        newrelic.recordMetric('Custom/NatsBus/gap_flush', 1);
        gap();
      }
      attempt = 0;

      // Client-level reconnects resubscribe automatically but drop whatever was
      // published while disconnected — flush on every reconnect notification.
      // The watcher dies with the connection; the outer loop replaces it.
      void (async () => {
        try {
          for await (const s of nc.status()) {
            if (s.type === 'reconnect') {
              newrelic.recordMetric('Custom/NatsBus/gap_flush', 1);
              gap();
            }
          }
        } catch {
          /* status iterator ends with the connection */
        }
      })();

      try {
        const sub = nc.subscribe(subject);
        for await (const m of sub) {
          try {
            onMsg(m.subject, m.data);
          } catch {
            /* per-message handler error — keep consuming */
          }
        }
      } catch {
        /* subscription iterator died — fall through to re-dial */
      }

      // Iterator ended: connection closed (shutdown) or errored. Back off and
      // loop; get() re-dials since the pooled conn is closed.
      attempt++;
      newrelic.recordMetric('Custom/NatsBus/resubscribe', 1);
      await sleep(backoffMs(attempt));
    }
  })();
}
