// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  connect,
  type ConnectionOptions,
  type NatsConnection,
  type Subscription
} from '@nats-io/transport-node';
import { tlsOptions } from '@bagel/kit/server/nats';

let conn: NatsConnection | null = null;
let dialing: Promise<NatsConnection> | null = null;

// `||`, not `??`: a set-but-blank var must fall through, or servers: '' dials nothing forever.
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
  // The hub runs mTLS: a CA-only config never connects and silently reconnect-loops.
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
