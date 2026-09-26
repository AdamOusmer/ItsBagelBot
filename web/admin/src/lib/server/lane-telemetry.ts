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

function pushConnection(bound: boolean | undefined): ConsumerConnection {
  if (bound === true) return 'bound';
  if (bound === false) return 'unbound';
  return 'unknown';
}

/** Push binding has no meaning for a pull consumer. Zero waiting requests
 * can also mean a pull worker is processing a batch, so it is not an orphan. */
function consumerConnection(ci: ConsumerTelemetry, mode: ConsumerMode): ConsumerConnection {
  if (mode === 'push') return pushConnection(ci.push_bound);
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

export function deliveryRate(previous: DeliverySample | undefined, current: DeliverySample): number | null {
  if (!previous || previous.created !== current.created || current.at <= previous.at || current.delivered < previous.delivered) {
    return null;
  }
  return (current.delivered - previous.delivered) * 1000 / (current.at - previous.at);
}

export function formatDeliveryRate(rate: number | null): string {
  if (rate === null) return '-';
  if (rate === 0) return '0 msg/s';
  if (rate < 0.1) return '<0.1 msg/s';
  if (rate < 10) return `${rate.toFixed(1)} msg/s`;
  return `${Math.round(rate)} msg/s`;
}

/** A JetStream Lister's next() returns one page, not the whole listing. */
export async function allPages<T>(lister: { next(): Promise<T[]> }): Promise<T[]> {
  const items: T[] = [];
  for (;;) {
    const page = await lister.next();
    if (page.length === 0) return items;
    items.push(...page);
  }
}
