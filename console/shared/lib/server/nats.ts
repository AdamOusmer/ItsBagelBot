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

// The console holds two connections on two accounts (per-account isolation):
//
//   * 'rpc'  — the per-service RPC account (NATS_RPC_USER/PASSWORD): request/reply
//     (bagel.rpc.*) and the cache-invalidation subscription (bagel.cache.*).
//   * 'bus'  — the shared BUS account (NATS_USER/PASSWORD): the JetStream lane
//     view (admin) and the outgress-system stream-feed publish (twitch.*).
//
// Both prefer the node-local leaf and fall back to the hub, ordered and never
// shuffled (noRandomize) so a reconnect always retries the leaf first.
type Role = 'rpc' | 'bus';

interface Pool {
  conn: NatsConnection | null;
  dialing: Promise<NatsConnection> | null;
}

const pools: Record<Role, Pool> = {
  rpc: { conn: null, dialing: null },
  bus: { conn: null, dialing: null }
};

// serverList returns the ordered endpoint list, leaf first then hub. `override`
// (NATS_RPC_URL for rpc, NATS_URL for bus) is used as the leaf endpoint when the
// explicit NATS_LEAF_URL/NATS_HUB_URL split is absent, so local dev and
// pre-migration deploys keep working against a single server.
function serverList(override: string | undefined): string[] {
  const leaf = process.env.NATS_LEAF_URL;
  const hub = process.env.NATS_HUB_URL;
  const fallback =
    override ?? `nats://${process.env.NATS_HOST ?? '127.0.0.1'}:${process.env.NATS_PORT ?? '4222'}`;
  if (!leaf && !hub) return [fallback];
  const list: string[] = [];
  list.push(leaf ?? fallback);
  if (hub && hub !== list[0]) list.push(hub);
  return list;
}

function options(role: Role): ConnectionOptions {
  const isRpc = role === 'rpc';
  const opts: ConnectionOptions = {
    servers: serverList(isRpc ? process.env.NATS_RPC_URL : process.env.NATS_URL),
    // Honor leaf-first order on the initial dial and every reconnect.
    noRandomize: true,
    name: `${process.env.NATS_CLIENT_NAME ?? 'console'}-${role}`,
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
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
      pool.conn = c;
      return c;
    })
    .finally(() => {
      pool.dialing = null;
    });
  return pool.dialing;
}

let jsClient: JetStreamClient | null = null;
let jsManager: JetStreamManager | null = null;

export async function js(): Promise<JetStreamClient> {
  const nc = await get('bus');
  if (!jsClient) jsClient = nc.jetstream({ domain: 'hub' });
  return jsClient;
}

export async function jsm(): Promise<JetStreamManager> {
  const nc = await get('bus');
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
