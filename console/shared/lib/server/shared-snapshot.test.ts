// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, test } from 'bun:test';
import { sharedSnapshot, setSnapshotClientForTests, type SnapshotClient } from './shared-snapshot';

afterEach(() => setSnapshotClientForTests(undefined));

/** A stand-in Valkey holding one string, with the calls it saw. */
function fakeClient(seed: string | null = null) {
  const calls = { get: 0, set: 0 };
  let stored = seed;
  const client: SnapshotClient = {
    async get() {
      calls.get++;
      return stored;
    },
    async set(_key, value) {
      calls.set++;
      stored = value;
      return 'OK';
    }
  };
  return { client, calls, read: () => stored };
}

const opts = <T>(load: () => Promise<T>, publish?: (v: T) => boolean) => ({
  key: 'test:snapshot:v1',
  ttlMs: 2_000,
  load,
  publish
});

describe('sharedSnapshot', () => {
  test('a stored snapshot is served without computing one', async () => {
    const { client, calls } = fakeClient(JSON.stringify({ n: 7 }));
    setSnapshotClientForTests(client);
    let loads = 0;

    const value = await sharedSnapshot(
      opts(async () => {
        loads++;
        return { n: 1 };
      })
    );

    expect(value).toEqual({ n: 7 });
    expect(loads).toBe(0);
    expect(calls.set).toBe(0);
  });

  test('a miss computes once and publishes it for the other pods', async () => {
    const store = fakeClient();
    setSnapshotClientForTests(store.client);

    const value = await sharedSnapshot(opts(async () => ({ n: 3 })));

    expect(value).toEqual({ n: 3 });
    expect(store.calls.set).toBe(1);
    expect(store.read()).toBe(JSON.stringify({ n: 3 }));
  });

  test('a value the caller refuses to publish is served but not stored', async () => {
    const store = fakeClient();
    setSnapshotClientForTests(store.client);

    const value = await sharedSnapshot(
      opts(
        async () => ({ degraded: true }),
        (v: { degraded: boolean }) => !v.degraded
      )
    );

    expect(value).toEqual({ degraded: true });
    expect(store.calls.set).toBe(0);
    expect(store.read()).toBeNull();
  });

  test('an unreadable payload falls back to a live read', async () => {
    const { client } = fakeClient('{not json');
    setSnapshotClientForTests(client);

    expect(await sharedSnapshot(opts(async () => ({ n: 5 })))).toEqual({ n: 5 });
  });

  test('a failing client never fails the read', async () => {
    setSnapshotClientForTests({
      get: () => Promise.reject(new Error('down')),
      set: () => Promise.reject(new Error('down'))
    });

    expect(await sharedSnapshot(opts(async () => ({ n: 9 })))).toEqual({ n: 9 });
  });

  test('no Valkey at all is just a local read', async () => {
    setSnapshotClientForTests(null);

    expect(await sharedSnapshot(opts(async () => ({ n: 4 })))).toEqual({ n: 4 });
  });
});
