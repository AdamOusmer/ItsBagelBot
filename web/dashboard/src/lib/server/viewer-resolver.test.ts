// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  BUCKET_CAPACITY,
  createViewerResolver,
  FOUND_TTL_MS,
  MAX_CACHED_LOGINS,
  MISSING_TTL_MS,
  type Resolution
} from './viewer-resolver';

function harness(answer: (login: string) => Promise<string> = async (login) => `id-${login}`, maxEntries?: number) {
  const clock = { now: 1_000_000 };
  const calls: string[] = [];
  const resolve = createViewerResolver({
    lookup: (login) => {
      calls.push(login);
      return answer(login);
    },
    now: () => clock.now,
    maxEntries
  });
  return { clock, calls, resolve };
}

const found = (viewerId: string): Resolution => ({ kind: 'found', viewerId });

describe('viewer resolver cache', () => {
  test('a found login is served from cache for 10 minutes then looked up again', async () => {
    const { clock, calls, resolve } = harness();
    expect(await resolve('sammy')).toEqual(found('id-sammy'));
    clock.now += FOUND_TTL_MS - 1;
    expect(await resolve('sammy')).toEqual(found('id-sammy'));
    expect(calls).toEqual(['sammy']);
    clock.now += 1;
    await resolve('sammy');
    expect(calls).toEqual(['sammy', 'sammy']);
  });

  test('a missing login is cached for 1 minute only', async () => {
    const { clock, calls, resolve } = harness(async () => '');
    expect(await resolve('ghost')).toEqual({ kind: 'missing' });
    clock.now += MISSING_TTL_MS - 1;
    expect(await resolve('ghost')).toEqual({ kind: 'missing' });
    expect(calls).toEqual(['ghost']);
    clock.now += 1;
    await resolve('ghost');
    expect(calls).toEqual(['ghost', 'ghost']);
  });

  test('an error is reported and never cached', async () => {
    let fail = true;
    const { calls, resolve } = harness(async (login) => {
      if (fail) throw new Error('outgress down');
      return `id-${login}`;
    });
    expect(await resolve('sammy')).toEqual({ kind: 'error' });
    fail = false;
    expect(await resolve('sammy')).toEqual(found('id-sammy'));
    expect(calls).toEqual(['sammy', 'sammy']);
  });

  test('concurrent lookups of one login share a single upstream call', async () => {
    let release: (id: string) => void = () => {};
    const { calls, resolve } = harness(() => new Promise<string>((done) => (release = done)));
    const first = resolve('sammy');
    const second = resolve('sammy');
    release('77');
    expect(await Promise.all([first, second])).toEqual([found('77'), found('77')]);
    expect(calls).toEqual(['sammy']);
  });

  test('the default cap is 5000 logins', () => {
    expect(MAX_CACHED_LOGINS).toBe(5000);
  });

  test('a full cache evicts the oldest login and keeps the rest', async () => {
    const { calls, resolve } = harness(undefined, 3);
    for (const login of ['aaaa', 'bbbb', 'cccc', 'dddd']) await resolve(login);
    calls.length = 0;
    await resolve('bbbb');
    await resolve('cccc');
    await resolve('dddd');
    expect(calls).toEqual([]);
    await resolve('aaaa');
    expect(calls).toEqual(['aaaa']);
  });
});

describe('viewer resolver login length', () => {
  test('a login under the Twitch minimum is missing without a lookup or a token', async () => {
    const { calls, resolve } = harness();
    for (const login of ['', 'a', 'sam']) expect(await resolve(login)).toEqual({ kind: 'missing' });
    expect(calls).toEqual([]);
    for (let i = 0; i < BUCKET_CAPACITY; i++) expect(await resolve(`vvv${i}`)).toEqual(found(`id-vvv${i}`));
  });

  test('a four character login is looked up', async () => {
    const { calls, resolve } = harness();
    expect(await resolve('sams')).toEqual(found('id-sams'));
    expect(calls).toEqual(['sams']);
  });
});

describe('viewer resolver token bucket', () => {
  test('30 uncached lookups pass then the next is busy without calling upstream', async () => {
    const { calls, resolve } = harness();
    for (let i = 0; i < BUCKET_CAPACITY; i++) await resolve(`vvv${i}`);
    expect(await resolve('extra')).toEqual({ kind: 'busy' });
    expect(calls).toHaveLength(BUCKET_CAPACITY);
  });

  test('cache hits and shared in-flight lookups spend no tokens', async () => {
    const { calls, resolve } = harness();
    await resolve('sammy');
    for (let i = 0; i < 100; i++) await resolve('sammy');
    for (let i = 0; i < BUCKET_CAPACITY - 1; i++) await resolve(`vvv${i}`);
    expect(calls).toHaveLength(BUCKET_CAPACITY);
  });

  test('the bucket refills half a token per second', async () => {
    const { clock, resolve } = harness();
    for (let i = 0; i < BUCKET_CAPACITY; i++) await resolve(`vvv${i}`);
    clock.now += 1999;
    expect(await resolve('aaaa')).toEqual({ kind: 'busy' });
    clock.now += 2000;
    expect(await resolve('bbbb')).toEqual(found('id-bbbb'));
    expect(await resolve('cccc')).toEqual({ kind: 'busy' });
  });

  test('a long idle period refills to capacity and no further', async () => {
    const { clock, calls, resolve } = harness();
    for (let i = 0; i < BUCKET_CAPACITY; i++) await resolve(`vvv${i}`);
    clock.now += 3_600_000;
    for (let i = 0; i < BUCKET_CAPACITY; i++) await resolve(`www${i}`);
    expect(await resolve('extra')).toEqual({ kind: 'busy' });
    expect(calls).toHaveLength(BUCKET_CAPACITY * 2);
  });

  test('an upstream error still spends its token', async () => {
    const { resolve } = harness(async () => {
      throw new Error('down');
    });
    for (let i = 0; i < BUCKET_CAPACITY; i++) expect(await resolve(`vvv${i}`)).toEqual({ kind: 'error' });
    expect(await resolve('extra')).toEqual({ kind: 'busy' });
  });
});
