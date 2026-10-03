// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, test } from 'bun:test';
import { isSessionRevoked, revokeAllForUser, revokeSession, setRevocationReadForTests } from './session-revocation';
import { resetMasterClientForTests } from './valkey-master';
import { registerServerConfig } from './config';

afterEach(() => setRevocationReadForTests(undefined));

type Claims = Parameters<typeof isSessionRevoked>[0];

describe('isSessionRevoked', () => {
  test.each([
    ['not revoked when neither key exists', { sid: 's1', userId: 'u1', iat: 1000 }, [null, null], false],
    ['revoked when the sid key is set', { sid: 's1', userId: 'u1', iat: 1000 }, ['1', null], true],
    ['revoked by the revoke-all epoch when iat predates it', { sid: 's1', userId: 'u1', iat: 1000 }, [null, '2000'], true],
    ['not revoked when iat is newer than the revoke-all epoch', { sid: 's1', userId: 'u1', iat: 3000 }, [null, '2000'], false],
    ['a sid-less legacy session is still revoked by the user epoch', { userId: '42', iat: 1_000 }, [null, '2000'], true],
    ['a sid-less legacy session issued after the epoch survives', { userId: '42', iat: 2_000 }, [null, '1000'], false]
  ] as [string, Claims, (string | null)[], boolean][])('%s', async (_name, claims, read, revoked) => {
    setRevocationReadForTests(async () => read as [string | null, string | null]);
    expect(await isSessionRevoked(claims)).toBe(revoked);
  });

  test('a missing sid still consults the user epoch, with no sid key', async () => {
    let askedFor: string | undefined = 'unset';
    setRevocationReadForTests(async ({ sid }) => {
      askedFor = sid;
      return [null, null];
    });
    expect(await isSessionRevoked({ userId: 'u1', iat: 1000 })).toBe(false);
    expect(askedFor).toBeUndefined();
  });

  test('fails open when the read is unavailable', async () => {
    setRevocationReadForTests(async () => {
      throw new Error('valkey unreachable');
    });
    expect(await isSessionRevoked({ sid: 's1', userId: 'u1', iat: 1000 })).toBe(false);
  });
});

describe('revokeSession / revokeAllForUser', () => {
  test('resolve without throwing when Valkey is configured but unreachable', async () => {
    resetMasterClientForTests();
    registerServerConfig({ valkey: { addr: '127.0.0.1:1' }, cacheInvalidationPrefix: 'test' });
    await expect(revokeSession('s1', 60)).resolves.toBeUndefined();
    await expect(revokeAllForUser('u1', 1000, 60)).resolves.toBeUndefined();
    resetMasterClientForTests();
  });
});
