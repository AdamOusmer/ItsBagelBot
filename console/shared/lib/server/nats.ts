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
  type JetStreamManager
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

let conn: NatsConnection | null = null;
let dialing: Promise<NatsConnection> | null = null;

// Server list: NATS_RPC_URL is preferred for request/reply so production can
// use the node-local leaf; NATS_URL remains the durable bus fallback.
function options(): ConnectionOptions {
  const server =
    process.env.NATS_RPC_URL ??
    process.env.NATS_URL ??
    `nats://${process.env.NATS_HOST ?? '127.0.0.1'}:${process.env.NATS_PORT ?? '4222'}`;
  const opts: ConnectionOptions = {
    servers: server,
    name: process.env.NATS_CLIENT_NAME ?? 'console',
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
    pingInterval: 20_000,
    // Bound the initial dial so a cold/unreachable NATS fails fast (and re-dials
    // on the next request) instead of hanging SSR to the 20s default and surfacing
    // a gateway "server connection error" the user has to refresh past.
    timeout: 3_000
  };
  if (process.env.NATS_USER) opts.user = process.env.NATS_USER;
  if (process.env.NATS_PASSWORD) opts.pass = process.env.NATS_PASSWORD;
  if (process.env.NATS_TOKEN) opts.token = process.env.NATS_TOKEN;
  return opts;
}

async function get(): Promise<NatsConnection> {
  if (conn && !conn.isClosed()) return conn;
  if (dialing) return dialing;
  // Clear `dialing` in finally, not in the success handler: if connect() rejects
  // (NATS down at dial time) the success handler never runs, so leaving it set
  // would pin a rejected promise here and fail every later request until the
  // process restarts. finally lets the next get() re-dial.
  dialing = connect(options())
    .then((c) => {
      conn = c;
      return c;
    })
    .finally(() => {
      dialing = null;
    });
  return dialing;
}

let jsClient: JetStreamClient | null = null;
let jsManager: JetStreamManager | null = null;

export async function js(): Promise<JetStreamClient> {
  const nc = await get();
  if (!jsClient) jsClient = nc.jetstream({ domain: 'hub' });
  return jsClient;
}

export async function jsm(): Promise<JetStreamManager> {
  const nc = await get();
  if (!jsManager) jsManager = await nc.jetstreamManager({ domain: 'hub' });
  return jsManager;
}

/**
 * Pre-dial the connection at server boot so the first user request hits a warm
 * conn instead of paying the cold dial on the hot path (the cause of the
 * intermittent "server connection error, refresh fixes it"). Best-effort: a
 * failed warm-up just leaves the next get() to re-dial.
 */
export function warm(): void {
  get().catch(() => {});
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
    const nc = await within(get(), timeoutMs);
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
    const nc = await get();
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
 */
export async function publish(subject: string, payload: unknown = {}): Promise<void> {
  return newrelic.startSegment(`NATS/publish/${rpcSegment(subject)}`, true, async () => {
    const nc = await get();
    nc.publish(subject, jc.encode(payload));
    await nc.flush();
  });
}

export async function closeNats(): Promise<void> {
  if (conn && !conn.isClosed()) await conn.drain();
  conn = null;
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
  get()
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
