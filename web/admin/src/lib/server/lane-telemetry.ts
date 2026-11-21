// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface ConsumerTelemetry {
  config: {
    deliver_subject?: string;
    ack_policy?: string;
    max_ack_pending?: number;
  };
  push_bound?: boolean;
  num_waiting: number;
  num_ack_pending: number;
  delivered: { consumer_seq: number };
}

type ConsumerMode = 'push' | 'pull';
type ConsumerConnection = 'bound' | 'unbound' | 'waiting' | 'unknown';

function consumerConnection(ci: ConsumerTelemetry, mode: ConsumerMode): ConsumerConnection {
  if (mode === 'push') return ci.push_bound ? 'bound' : 'unbound';
  // A pull worker with no waiting requests can still be active and must remain protected from deletion.
  return ci.num_waiting > 0 ? 'waiting' : 'unknown';
}

export function consumerActivity(ci: ConsumerTelemetry) {
  const mode: ConsumerMode = ci.config.deliver_subject ? 'push' : 'pull';
  const connection = consumerConnection(ci, mode);
  return {
    mode,
    connection,
    orphan: connection === 'unbound',
    delivered: ci.delivered.consumer_seq,
    waiting: ci.num_waiting,
    ackPolicy: ci.config.ack_policy || 'explicit',
    ackPending: ci.num_ack_pending,
    maxAckPending: ci.config.max_ack_pending || 0
  };
}

export interface DeliverySample {
  delivered: number;
  at: number;
  created: string;
}

function comparableDeliverySamples(previous: DeliverySample, current: DeliverySample): boolean {
  if (previous.created !== current.created) return false;
  if (current.at <= previous.at) return false;
  return current.delivered >= previous.delivered;
}

export function deliveryRate(previous: DeliverySample | undefined, current: DeliverySample): number | null {
  if (!previous) return null;
  if (!comparableDeliverySamples(previous, current)) return null;
  return (current.delivered - previous.delivered) * 1000 / (current.at - previous.at);
}

export function formatDeliveryRate(rate: number | null): string {
  if (rate === null) return '-';
  if (rate === 0) return '0 msg/s';
  if (rate < 0.1) return '<0.1 msg/s';
  if (rate < 10) return `${rate.toFixed(1)} msg/s`;
  return `${Math.round(rate)} msg/s`;
}

type LaneLister<T> = {
  next(): Promise<T[]>;
  pageInfo?: { missing?: string[] };
};

export async function allPages<T>(lister: LaneLister<T>): Promise<T[]> {
  const items: T[] = [];
  for (;;) {
    const page = await lister.next();
    if (lister.pageInfo?.missing?.length) throw new Error('JetStream listing omitted unavailable entries');
    if (page.length === 0) return items;
    items.push(...page);
  }
}
