// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Live status feed: a process-wide NATS connection used only for the wildcard
// subscription that drives the SSE endpoint. Kept separate from the shared
// request/reply client so a long-lived subscription never interferes with the
// short-lived RPC requests (and vice versa).
import {
import { tlsOptions } from '@bagel/kit/server/nats';
  connect,
  type ConnectionOptions,
  type NatsConnection,
  type Subscription
} from '@nats-io/transport-node';

let conn: NatsConnection | null = null;
let dialing: Promise<NatsConnection> | null = null;

// The status stream lives on the hub (like the shared client's bus role);
// NATS_URL is the local-dev fallback only.
// `||` chains, not `??` (same blank-as-absent rule as the SUB maps): a
// set-but-blank var must fall through, or servers: '' dials nothing forever.
function url(): string {
  return process.env.NATS_HUB_URL || process.env.NATS_URL || 'nats://127.0.0.1:4222';
}

async function get(): Promise<NatsConnection> {
  if (conn && !conn.isClosed()) return conn;
  if (dialing) return dialing;
  const opts: ConnectionOptions = {
    servers: url(),
    name: (process.env.NATS_CLIENT_NAME || 'console') + '-feed',
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
    pingInterval: 20_000,
    ignoreAuthErrorAbort: true,
    timeout: 3_000
  };
  if (process.env.NATS_USER) opts.user = process.env.NATS_USER;
  if (process.env.NATS_PASSWORD) opts.pass = process.env.NATS_PASSWORD;
  if (process.env.NATS_TOKEN) opts.token = process.env.NATS_TOKEN;
  // Same TLS shape as the shared client: fleet CA to verify the server AND the
  // cert-manager client pair, because the hub runs mTLS. CA-only (what this
  // did until 2026-09-10) never connects: the hub logged "client didn't
  // provide a certificate" every ~6s per admin pod, a permanent reconnect loop
  // that never surfaced in the console. No CA (local dev) stays plaintext.
  const tls = tlsOptions();
  if (tls) opts.tls = tls;

  dialing = connect(opts)
    .then((c) => {
      conn = c;
      return c;
    })
    .finally(() => {
      dialing = null;
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
