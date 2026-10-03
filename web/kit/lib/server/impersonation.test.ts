// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { generateKeyPairSync, sign } from 'node:crypto';
import { signViewAs, verifyViewAs, type ViewAsPayload } from './impersonation';

const { privateKey, publicKey } = generateKeyPairSync('ed25519');
const privateKeyB64 = privateKey.export({ type: 'pkcs8', format: 'der' }).toString('base64');
const publicKeyB64 = publicKey.export({ type: 'spki', format: 'der' }).toString('base64');
const base = {
  sub: '42',
  login: 'bagel',
  display_name: 'Bagel',
  by_id: '7',
  by_login: 'admin'
};

function signedPayload(payload: ViewAsPayload): string {
  const body = Buffer.from(JSON.stringify(payload), 'utf8').toString('base64url');
  const signature = sign(null, Buffer.from(body, 'utf8'), privateKey).toString('base64url');
  return `${body}.${signature}`;
}

describe('view-as tokens', () => {
  test('signs and verifies a valid token', () => {
    const token = signViewAs(base, { privateKey: privateKeyB64, now: () => 1_000, uuid: () => 'nonce' });
    expect(verifyViewAs(token, { publicKey: publicKeyB64, now: () => 1_001 })).toMatchObject({
      ...base,
      jti: 'nonce'
    });
  });

  test.each([
    ['rejects a malformed token', () => 'not-a-token', 1_001],
    ['rejects a tampered signature', () => `${signViewAs(base, { privateKey: privateKeyB64, now: () => 1_000 })}x`, 1_001],
    ['rejects the wrong audience', () => signedPayload({
      ...base,
      iss: 'bagel-console-admin',
      aud: 'not-the-dashboard' as ViewAsPayload['aud'],
      jti: 'nonce',
      iat: 1_000,
      exp: 1_300
    }), 1_001],
    ['rejects excessive lifetimes', () => signViewAs({ ...base, exp: 1_301 }, { privateKey: privateKeyB64, now: () => 1_000 }), 1_001],
    ['rejects expired tokens', () => signViewAs(base, { privateKey: privateKeyB64, now: () => 1_000 }), 1_301]
  ] as [string, () => string, number][])('%s', (_name, mint, now) => {
    expect(verifyViewAs(mint(), { publicKey: publicKeyB64, now: () => now })).toBeNull();
  });
});
