// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { expect, mock, test } from 'bun:test';
import type { FeedHistory } from '../src/lib/server/feed-history';

const prefix = 'custom.status';
const stream = 'CUSTOM_EVENTS';
const find = mock(async (_filter: string) => stream);
const subscribe = mock();
const flush = mock(async () => {});
const replay = {
  async *[Symbol.asyncIterator]() {
    yield {
      subject: `${prefix}.shard.up`,
      data: new TextEncoder().encode('published'),
      timestamp: '2026-11-20T00:00:00Z'
    };
  },
  close: async () => {}
};
const getConsumer = mock(async (_name: string, _options: { filter_subjects: string }) => ({
  info: async () => ({ num_pending: 1 }),
  fetch: async () => replay,
  delete: async () => {}
}));

mock.module('@nats-io/transport-node', () => ({
  connect: async () => ({ isClosed: () => false, subscribe, flush })
}));
mock.module('@nats-io/jetstream', () => ({
  jetstream: () => ({ consumers: { get: getConsumer } })
}));
mock.module('@bagel/kit/server/nats', () => ({
  credentialAuth: () => ({}),
  enableHubFailback: () => {},
  hubJetStreamOptions: () => ({}),
  jsm: async () => ({ streams: { find } }),
  tlsOptions: () => undefined
}));

const { statusHistory } = await import('../src/lib/server/feed');

function subscription() {
  const done = Promise.withResolvers<IteratorResult<never>>();
  let closed = false;
  const unsubscribe = () => {
    closed = true;
    done.resolve({ done: true, value: undefined });
  };
  return {
    isClosed: () => closed,
    unsubscribe,
    [Symbol.asyncIterator]: () => ({ next: () => done.promise })
  };
}

function observe(hub: FeedHistory) {
  const labels: string[] = [];
  const closed = Promise.withResolvers<void>();
  hub.listen({
    history: (rows) => labels.push(...rows.map((row) => row.label)),
    feed: () => {},
    close: () => closed.resolve()
  });
  return { labels, closed: closed.promise };
}

test('resolves the configured subject and replays it after a hub release', async () => {
  const first = subscription();
  const second = subscription();
  subscribe.mockReturnValueOnce(first).mockReturnValueOnce(second);

  const initial = statusHistory(prefix);
  expect(statusHistory(prefix)).toBe(initial);
  const firstHub = await initial;
  const original = observe(firstHub);
  first.unsubscribe();
  await original.closed;

  const nextHub = await statusHistory(prefix);
  const restarted = observe(nextHub);
  second.unsubscribe();
  await restarted.closed;

  const filter = `${prefix}.>`;
  expect(nextHub.generation).not.toBe(firstHub.generation);
  expect({
    lookup: find.mock.calls,
    consumers: getConsumer.mock.calls.map(([name, options]) => [name, options.filter_subjects]),
    histories: [original.labels, restarted.labels]
  }).toEqual({
    lookup: [[filter], [filter]],
    consumers: [[stream, filter], [stream, filter]],
    histories: [['shard.up'], ['shard.up']]
  });
});

test('a failed subscription round-trip rejects readiness and releases the subscription', async () => {
  const unacknowledged = subscription();
  subscribe.mockReturnValueOnce(unacknowledged);
  flush.mockRejectedValueOnce(new Error('flush failed'));

  await expect(statusHistory(prefix)).rejects.toThrow('flush failed');
  expect(unacknowledged.isClosed()).toBe(true);
});
