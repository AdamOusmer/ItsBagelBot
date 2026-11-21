// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  connect,
  type ConnectionOptions,
  type NatsConnection,
  type Subscription
} from '@nats-io/transport-node';
import { jetstream, type ConsumerMessages } from '@nats-io/jetstream';
import { credentialAuth, enableHubFailback, hubJetStreamOptions, tlsOptions } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { withTimeout } from '@bagel/kit/server/resilience';
import { FeedHistory, FeedRing, type FeedRecord } from './feed-history';

export type { FeedEvent } from './feed-history';

type StatusPrefix = string;

let conn: NatsConnection | null = null;
let dialing: Promise<NatsConnection> | null = null;
let active: Promise<FeedHistory> | undefined;
const history = new FeedHistory();
const decoder = new TextDecoder();
const HISTORY_AGE_MS = 5 * 60_000;

function url(): string {
  return process.env.NATS_HUB_URL || process.env.NATS_URL || 'nats://127.0.0.1:4222';
}

export function feedOptions(): ConnectionOptions {
  const opts: ConnectionOptions = {
    servers: url(),
    name: (process.env.NATS_CLIENT_NAME || 'console') + '-feed',
    maxReconnectAttempts: -1,
    reconnectTimeWait: 500,
    pingInterval: 20_000,
    ignoreAuthErrorAbort: true,
    timeout: 3_000
  };
  Object.assign(opts, credentialAuth(process.env.NATS_JWT, process.env.NATS_NKEY_SEED));
  if (process.env.NATS_TOKEN) opts.token = process.env.NATS_TOKEN;
  const tls = tlsOptions();
  if (tls) opts.tls = tls;
  return opts;
}

async function get(): Promise<NatsConnection> {
  if (conn && !conn.isClosed()) return conn;
  if (dialing) return dialing;

  dialing = connect(feedOptions())
    .then((c) => {
      conn = c;
      enableHubFailback(c, feedOptions());
      return c;
    })
    .finally(() => {
      dialing = null;
    });
  return dialing;
}

function toneFor(subject: FeedRecord['subject']): FeedRecord['tone'] {
  const last = subject.split('.').pop() ?? '';
  if (['up', 'online', 'connected', 'ready', 'ok', 'bound'].includes(last)) return 'up';
  if (['down', 'offline', 'lost', 'disconnected', 'degraded', 'error', 'failed'].includes(last))
    return 'down';
  return 'neutral';
}

export async function subscribeStatus(prefix: StatusPrefix, connection?: NatsConnection): Promise<Subscription> {
  return (connection ?? await get()).subscribe(`${prefix}.>`);
}

function decode(prefix: StatusPrefix, message: { subject: string; data: Uint8Array }, at: FeedRecord['at']): FeedRecord {
  const { subject, data } = message;
  return {
    subject,
    label: subject.startsWith(prefix + '.') ? subject.slice(prefix.length + 1) : subject,
    tone: toneFor(subject),
    payload: decoder.decode(data),
    at
  };
}

async function loadHistory(nc: NatsConnection, prefix: StatusPrefix, startedAt: number): Promise<FeedRecord[]> {
  const js = jetstream(nc, { ...hubJetStreamOptions(), timeout: 1_500 });
  const consumer = await js.consumers.get('TWITCH_INGRESS', {
    filter_subjects: `${prefix}.>`,
    opt_start_time: new Date(startedAt - HISTORY_AGE_MS).toISOString()
  });
  try {
    const { num_pending } = await consumer.info(true);
    if (!num_pending) return [];
    const messages = await consumer.fetch({ max_messages: num_pending, expires: 1_000 });
    return await withTimeout(readReplay(messages, prefix), 2_000, 'admin event history')
      .finally(() => messages.close());
  } finally {
    void consumer.delete().catch((err) => logger.warn({ err }, 'admin event history consumer cleanup failed'));
  }
}

async function readReplay(messages: ConsumerMessages, prefix: StatusPrefix): Promise<FeedRecord[]> {
  const records = new FeedRing<FeedRecord>();
  for await (const message of messages) records.add(decode(prefix, message, message.timestamp));
  return records.values();
}

async function collectStatus(sub: Subscription, prefix: StatusPrefix): Promise<void> {
  try {
    for await (const message of sub) history.add(decode(prefix, message, new Date().toISOString()));
  } catch (err) {
    logger.warn({ err }, 'admin event subscription closed');
  }
}

async function startStatus(prefix: StatusPrefix, closed: () => void): Promise<FeedHistory> {
  const nc = await get();
  const startedAt = Date.now();
  const sub = await subscribeStatus(prefix, nc);
  const ready = history.seed(new Date(startedAt).toISOString(), () => loadHistory(nc, prefix, startedAt));
  void collectStatus(sub, prefix).then(closed);
  await ready;
  if (sub.isClosed()) throw new Error('admin event subscription closed');
  return history;
}

export function statusHistory(prefix: StatusPrefix): Promise<FeedHistory> {
  if (active) return active;
  const release = () => {
    if (active !== start) return;
    active = undefined;
    history.disconnect();
  };
  const start = startStatus(prefix, release);
  active = start;
  void start.catch(release);
  return start;
}
