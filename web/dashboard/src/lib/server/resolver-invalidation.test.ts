// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { createViewerResolver } from './viewer-resolver';

test('rename invalidation drops found and missing logins while preserving other entries', async () => {
  const calls: string[] = [];
  const resolve = createViewerResolver({ lookup: async (login) => {
    calls.push(login);
    return login === 'newname' ? '' : '42';
  } });
  for (const login of ['oldname', 'newname', 'unrelated']) await resolve(login);
  resolve.invalidate(['oldname', 'newname']);
  for (const login of ['oldname', 'newname', 'unrelated']) await resolve(login);
  expect(calls).toEqual(['oldname', 'newname', 'unrelated', 'oldname', 'newname']);
});

test('an invalidated in-flight lookup cannot refill or remove its replacement', async () => {
  const pending: Array<(value: string) => void> = [];
  const resolve = createViewerResolver({ lookup: () => new Promise((done) => pending.push(done)) });
  const stale = resolve('oldname');
  resolve.invalidate(['oldname']);
  const fresh = resolve('oldname');
  pending[0]!('stale');
  await stale;
  const shared = resolve('oldname');
  expect(pending).toHaveLength(2);
  pending[1]!('fresh');
  expect(await Promise.all([fresh, shared, resolve('oldname')])).toEqual([
    { kind: 'found', viewerId: 'fresh' }, { kind: 'found', viewerId: 'fresh' }, { kind: 'found', viewerId: 'fresh' }
  ]);
});
