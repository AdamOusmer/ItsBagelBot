// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import { allPages, consumerActivity, deliveryRate, formatDeliveryRate, type ConsumerTelemetry } from '../src/lib/server/lane-telemetry';

function consumer(overrides: Partial<ConsumerTelemetry> = {}): ConsumerTelemetry {
  return {
    config: { ack_policy: 'explicit', max_ack_pending: 2000 },
    push_bound: false,
    num_waiting: 0,
    num_ack_pending: 0,
    delivered: { consumer_seq: 25_000 },
    ...overrides
  };
}

describe('lane telemetry', () => {
  test('low activity never rounds to an idle-looking zero', () => {
    expect(formatDeliveryRate(null)).toBe('-');
    expect(formatDeliveryRate(0)).toBe('0 msg/s');
    expect(formatDeliveryRate(1 / 60)).toBe('<0.1 msg/s');
    expect(formatDeliveryRate(0.049)).toBe('<0.1 msg/s');
    expect(formatDeliveryRate(0.1)).toBe('0.1 msg/s');
    expect(formatDeliveryRate(5.25)).toBe('5.3 msg/s');
    expect(formatDeliveryRate(100)).toBe('100 msg/s');
  });
  test('zero ACK occupancy can accompany substantial activity', () => {
    const activity = consumerActivity(consumer());
    expect(activity.ackPending).toBe(0);
    expect(activity.maxAckPending).toBe(2000);
    expect(activity.delivered).toBe(25_000);
    expect(deliveryRate(
      { delivered: 20_000, at: 0, created: 'one' },
      { delivered: activity.delivered, at: 5000, created: 'one' }
    )).toBe(1000);
  });

  test('pull consumers are never declared orphan from push binding', () => {
    expect(consumerActivity(consumer())).toMatchObject({ mode: 'pull', connection: 'unknown', orphan: false });
    expect(consumerActivity(consumer({ num_waiting: 8 }))).toMatchObject({ mode: 'pull', connection: 'waiting', waiting: 8, orphan: false });
  });

  test('push orphan detection requires an explicitly absent binding', () => {
    const config = { deliver_subject: '_INBOX.worker' };
    expect(consumerActivity(consumer({ config }))).toMatchObject({ mode: 'push', connection: 'unbound', orphan: true });
    expect(consumerActivity(consumer({ config, push_bound: true }))).toMatchObject({ connection: 'bound', orphan: false });
    expect(consumerActivity(consumer({ config, push_bound: undefined }))).toMatchObject({ connection: 'unknown', orphan: false });
  });

  test('ack-none consumers have no artificial ACK capacity', () => {
    expect(consumerActivity(consumer({ config: { ack_policy: 'none' } }))).toMatchObject({ ackPolicy: 'none', maxAckPending: 0 });
  });

  test('idle is zero, while missing or reset rate samples are unavailable', () => {
    const sample = { delivered: 100, at: 5000, created: 'one' };
    expect(deliveryRate(undefined, sample)).toBeNull();
    expect(deliveryRate(sample, { ...sample, at: 10000 })).toBe(0);
    expect(deliveryRate(sample, { ...sample, delivered: 10, at: 10000 })).toBeNull();
    expect(deliveryRate(sample, { ...sample, created: 'two', at: 10000 })).toBeNull();
    expect(deliveryRate(sample, sample)).toBeNull();
  });

  test('collects every NATS API page', async () => {
    const pages = [[1, 2], [3], []];
    expect(await allPages({ next: async () => pages.shift()! })).toEqual([1, 2, 3]);
    expect(pages).toHaveLength(0);
  });

  test('a later-page API failure is surfaced rather than appearing complete', async () => {
    let count = 0;
    await expect(allPages({ next: async () => {
      if (count++ === 0) return ['first'];
      throw new Error('consumer listing failed');
    } })).rejects.toThrow('consumer listing failed');
  });
});
