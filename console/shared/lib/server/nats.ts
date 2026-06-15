// Server-only NATS RPC client. One lazily-dialed, process-wide connection
// reused across requests (connection setup is the expensive part; a warm conn
// keeps request/reply in the low-ms range, which is what the p99 budget needs).
import { connect, JSONCodec, type ConnectionOptions, type NatsConnection } from 'nats';

const jc = JSONCodec();

let conn: NatsConnection | null = null;
let dialing: Promise<NatsConnection> | null = null;

// Server list: explicit NATS_URL wins, else built from NATS_HOST/NATS_PORT
// (cluster default nats:4222). The cluster NATS is authenticated, so credentials
// (NATS_USER/NATS_PASSWORD or NATS_TOKEN) are passed through when present.
function options(): ConnectionOptions {
  const server =
    process.env.NATS_URL ??
    `nats://${process.env.NATS_HOST ?? '127.0.0.1'}:${process.env.NATS_PORT ?? '4222'}`;
  const opts: ConnectionOptions = {
    servers: server,
    name: process.env.NATS_CLIENT_NAME ?? 'console',
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
    pingInterval: 20_000
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

export class RpcError extends Error {}

/**
 * Core NATS request/reply with a JSON body. Rejects on transport failure, on a
 * missing responder, or when the reply carries an `error` field. `timeoutMs`
 * defaults to 5s to match the Go callers.
 */
export async function rpc<T>(subject: string, payload: unknown = {}, timeoutMs = 5000): Promise<T> {
  const nc = await get();
  const msg = await nc.request(subject, jc.encode(payload), { timeout: timeoutMs });
  const reply = jc.decode(msg.data) as T & { error?: string };
  if (reply && typeof reply === 'object' && reply.error) throw new RpcError(reply.error);
  return reply as T;
}

/**
 * Fire-and-forget JSON publish (e.g. the outgress system lane). JetStream
 * streams capture core publishes to their subjects, so this enqueues the job
 * without needing a JetStream client.
 */
export async function publish(subject: string, payload: unknown = {}): Promise<void> {
  const nc = await get();
  nc.publish(subject, jc.encode(payload));
  await nc.flush();
}

export async function closeNats(): Promise<void> {
  if (conn && !conn.isClosed()) await conn.drain();
  conn = null;
}
