// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, test } from 'bun:test';
import { isSessionRevoked, revokeAllForUser, revokeSession, setRevocationReadForTests } from './session-revocation';
import { resetMasterClientForTests } from './valkey-master';
import { registerServerConfig } from './config';

afterEach(() => setRevocationReadForTests(undefined));

describe('isSessionRevoked', () => {
  test('not revoked when neither key exists', async () => {
    setRevocationReadForTests(async () => [null, null]);
    expect(await isSessionRevoked({ sid: 's1', userId: 'u1', iat: 1000 })).toBe(false);
  });

  test('revoked when the sid key is set', async () => {
    setRevocationReadForTests(async () => ['1', null]);
    expect(await isSessionRevoked({ sid: 's1', userId: 'u1', iat: 1000 })).toBe(true);
  });

  test('revoked by the revoke-all epoch when iat predates it', async () => {
    setRevocationReadForTests(async () => [null, '2000']);
    expect(await isSessionRevoked({ sid: 's1', userId: 'u1', iat: 1000 })).toBe(true);
  });

  test('not revoked when iat is newer than the revoke-all epoch', async () => {
    setRevocationReadForTests(async () => [null, '2000']);
    expect(await isSessionRevoked({ sid: 's1', userId: 'u1', iat: 3000 })).toBe(false);
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

  test('a sid-less legacy session is still revoked by the user epoch', async () => {
    setRevocationReadForTests(async ({ sid }) => {
      expect(sid).toBeUndefined();
      return [null, String(2_000)];
    });
    expect(await isSessionRevoked({ userId: '42', iat: 1_000 })).toBe(true);
  });

  test('a sid-less legacy session issued after the epoch survives', async () => {
    setRevocationReadForTests(async () => [null, String(1_000)]);
    expect(await isSessionRevoked({ userId: '42', iat: 2_000 })).toBe(false);
  });
});