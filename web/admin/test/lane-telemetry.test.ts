// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import {
  allPages,
  consumerActivity,
  deliveryRate,
  formatDeliveryRate,
  type ConsumerTelemetry,
  type DeliverySample
} from '../src/lib/server/lane-telemetry';

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
  test.each([
    [null, '-'],
    [0, '0 msg/s'],
    [1 / 60, '<0.1 msg/s'],
    [0.049, '<0.1 msg/s'],
    [0.1, '0.1 msg/s'],
    [5.25, '5.3 msg/s'],
    [100, '100 msg/s']
  ])('formats the delivery rate %s', (rate: number | null, expected: string) => {
    expect(formatDeliveryRate(rate)).toBe(expected);
  });

  test('zero ACK occupancy can accompany substantial activity', () => {
    const activity = consumerActivity(consumer());
    expect(activity).toMatchObject({ ackPending: 0, maxAckPending: 2000, delivered: 25_000 });
    expect(deliveryRate(
      { delivered: 20_000, at: 0, created: 'one' },
      { delivered: activity.delivered, at: 5000, created: 'one' }
    )).toBe(1000);
  });

  const push = { deliver_subject: '_INBOX.worker' };

  test.each([
    ['pull with no waiting requests', {}, { mode: 'pull', connection: 'unknown', orphan: false }],
    ['pull with waiting requests', { num_waiting: 8 }, { mode: 'pull', connection: 'waiting', waiting: 8, orphan: false }],
    ['unbound push', { config: push }, { mode: 'push', connection: 'unbound', orphan: true }],
    ['bound push', { config: push, push_bound: true }, { mode: 'push', connection: 'bound', orphan: false }],
    ['unbound push with an omitted flag', { config: push, push_bound: undefined }, { mode: 'push', connection: 'unbound', orphan: true }]
  ])('classifies a %s consumer', (
    _label: string,
    overrides: Partial<ConsumerTelemetry>,
    expected: Partial<ReturnType<typeof consumerActivity>>
  ) => {
    expect(consumerActivity(consumer(overrides))).toMatchObject(expected);
  });

  test('ack-none consumers have no artificial ACK capacity', () => {
    expect(consumerActivity(consumer({ config: { ack_policy: 'none' } }))).toMatchObject({ ackPolicy: 'none', maxAckPending: 0 });
  });

  const sample = { delivered: 100, at: 5000, created: 'one' };

  test.each([
    ['missing baseline', undefined, sample, null],
    ['idle consumer', sample, { ...sample, at: 10000 }, 0],
    ['reset counter', sample, { ...sample, delivered: 10, at: 10000 }, null],
    ['replacement consumer', sample, { ...sample, created: 'two', at: 10000 }, null],
    ['repeated time', sample, sample, null]
  ])('calculates the rate with %s', (
    _label: string,
    previous: DeliverySample | undefined,
    current: DeliverySample,
    expected: number | null
  ) => {
    expect(deliveryRate(previous, current)).toBe(expected);
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

  test('a successful page that omits offline consumers still fails the listing', async () => {
    const pageInfo = { missing: [] as string[] };
    const lister = {
      pageInfo,
      next: async () => {
        pageInfo.missing.push('offline');
        return ['available'];
      }
    };
    await expect(allPages(lister)).rejects.toThrow('JetStream listing omitted unavailable entries');
  });
});
