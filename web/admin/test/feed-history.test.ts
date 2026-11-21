// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import {
  FeedHistory,
  type FeedEvent,
  type FeedListener,
  type FeedRecord
} from '../src/lib/server/feed-history';

const instant = (offset: number) => new Date(1_800_000_000_000 + offset).toISOString();

function record(offset: number, payload = String(offset)): FeedRecord {
  return {
    at: instant(offset),
    subject: 'twitch.ingress.status.shard.up',
    label: 'shard.up',
    tone: 'up',
    payload
  };
}

type Delivery = ['history' | 'feed', FeedEvent[]] | ['close'];

function listening(hub: FeedHistory) {
  const deliveries: Delivery[] = [];
  const listener: FeedListener = {
    history: (events) => deliveries.push(['history', events]),
    feed: (event) => deliveries.push(['feed', [event]]),
    close: () => deliveries.push(['close'])
  };
  const stop = hub.listen(listener);
  return { deliveries, stop };
}

describe('feed history', () => {
  test.each([0, 3, 500, 501, 1_003])('keeps the latest 500 events after %i publishes', (size: number) => {
    const hub = new FeedHistory();
    for (let i = 1; i <= size; i++) hub.add(record(i));
    const { deliveries } = listening(hub);
    hub.add(record(size + 1));

    const expected = Array.from({ length: Math.min(500, size) }, (_, i) => {
      const id = size - i;
      return { ...record(id), id };
    });
    expect(deliveries).toEqual([
      ['history', expected],
      ['feed', [{ ...record(size + 1), id: size + 1 }]]
    ]);
  });

  test('fans out with stable ids and removes canceled listeners', () => {
    const hub = new FeedHistory();
    const first = listening(hub);
    hub.add(record(1));
    const second = listening(hub);
    first.stop();
    hub.add(record(2));
    hub.disconnect();

    const firstEvent = { ...record(1), id: 1 };
    expect(first.deliveries).toEqual([['history', []], ['feed', [firstEvent]]]);
    expect(second.deliveries).toEqual([
      ['history', [firstEvent]],
      ['feed', [{ ...record(2), id: 2 }]],
      ['close']
    ]);
  });

  test.each([
    { name: 'available', replay: [record(-5, 'same'), record(1, 'same'), record(2, 'seed only')], expected: ['new', 'seed only', 'same', 'same'] },
    { name: 'empty', replay: [], expected: ['new', 'same'] },
    { name: 'unavailable', replay: null, expected: ['new', 'same'] }
  ])('seeds once with $name replay and keeps concurrent live events', async ({ replay, expected }: {
    replay: readonly FeedRecord[] | null;
    expected: readonly string[];
  }) => {
    const hub = new FeedHistory();
    let attempts = 0;
    const load = async () => {
      attempts++;
      if (!replay) throw new Error('seed unavailable');
      return [...replay];
    };
    const initial = hub.seed(instant(0), load);
    hub.add(record(1, 'same'));
    hub.add(record(3, 'new'));
    await Promise.all([initial, hub.seed(instant(4), load)]);
    await hub.seed(instant(5), load);

    const { deliveries } = listening(hub);
    hub.add(record(6, 'latest'));
    expect(attempts).toBe(1);
    expect(deliveries.map(([kind, events]) => [kind, events?.map(({ payload, id }) => [payload, id])])).toEqual([
      ['history', expected.map((payload, i) => [payload, expected.length - i])],
      ['feed', [['latest', expected.length + 1]]]
    ]);
  });
});
