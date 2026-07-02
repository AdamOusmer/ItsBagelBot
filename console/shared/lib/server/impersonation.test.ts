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

  test('rejects malformed and tampered signatures', () => {
    expect(verifyViewAs('not-a-token', { publicKey: publicKeyB64 })).toBeNull();
    const token = signViewAs(base, { privateKey: privateKeyB64, now: () => 1_000 });
    expect(verifyViewAs(`${token}x`, { publicKey: publicKeyB64, now: () => 1_001 })).toBeNull();
  });

  test('rejects the wrong audience', () => {
    const token = signedPayload({
      ...base,
      iss: 'bagel-console-admin',
      aud: 'not-the-dashboard' as ViewAsPayload['aud'],
      jti: 'nonce',
      iat: 1_000,
      exp: 1_300
    });
    expect(verifyViewAs(token, { publicKey: publicKeyB64, now: () => 1_001 })).toBeNull();
  });

  test('rejects excessive lifetimes', () => {
    const token = signViewAs({ ...base, exp: 1_301 }, {
      privateKey: privateKeyB64,
      now: () => 1_000
    });
    expect(verifyViewAs(token, { publicKey: publicKeyB64, now: () => 1_001 })).toBeNull();
  });

  test('rejects expired tokens', () => {
    const token = signViewAs(base, { privateKey: privateKeyB64, now: () => 1_000 });
    expect(verifyViewAs(token, { publicKey: publicKeyB64, now: () => 1_301 })).toBeNull();
  });
});
