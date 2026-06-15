// Live status feed: a process-wide NATS connection used only for the wildcard
// subscription that drives the SSE endpoint. Kept separate from the shared
// request/reply client so a long-lived subscription never interferes with the
// short-lived RPC requests (and vice versa).
import { connect, type NatsConnection, type Subscription } from 'nats';

let conn: NatsConnection | null = null;
let dialing: Promise<NatsConnection> | null = null;

function url(): string {
  return process.env.NATS_URL ?? 'nats://127.0.0.1:4222';
}

async function get(): Promise<NatsConnection> {
  if (conn && !conn.isClosed()) return conn;
  if (dialing) return dialing;
  dialing = connect({
    servers: url(),
    name: (process.env.NATS_CLIENT_NAME ?? 'console') + '-feed',
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
    pingInterval: 20_000
  }).then((c) => {
    conn = c;
    dialing = null;
    return c;
  });
  return dialing;
}

export interface FeedEvent {
  subject: string;
  label: string;
  tone: 'up' | 'down' | 'neutral';
  payload: string;
  time: string;
}

function toneFor(subject: string): FeedEvent['tone'] {
  const last = subject.split('.').pop() ?? '';
  if (['up', 'online', 'connected', 'ready', 'ok', 'bound'].includes(last)) return 'up';
  if (['down', 'offline', 'lost', 'disconnected', 'degraded', 'error', 'failed'].includes(last))
    return 'down';
  return 'neutral';
}

// subscribe opens a wildcard subscription under `${prefix}.>` and yields a
// decoded FeedEvent per message. The caller iterates and is responsible for
// unsubscribing (e.g. on stream cancel).
export async function subscribeStatus(prefix: string): Promise<Subscription> {
  const nc = await get();
  return nc.subscribe(`${prefix}.>`);
}

export function decode(prefix: string, subject: string, data: Uint8Array): FeedEvent {
  let payload = new TextDecoder().decode(data).trim();
  if (payload.length > 240) payload = payload.slice(0, 240) + '…';
  return {
    subject,
    label: subject.startsWith(prefix + '.') ? subject.slice(prefix.length + 1) : subject,
    tone: toneFor(subject),
    payload,
    time: new Date().toLocaleTimeString('en-GB', { hour12: false })
  };
}
