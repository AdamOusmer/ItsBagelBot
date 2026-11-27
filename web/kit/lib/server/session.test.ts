// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { randomBytes, createCipheriv } from 'node:crypto';
import { createSessionCodec, decodeKey, type SessionBase } from './session';

interface TestSession extends SessionBase {
  user_id: string;
}

const key = randomBytes(32);
const codec = createSessionCodec<TestSession>(() => key, 'test-session');
const now = () => Math.floor(Date.now() / 1000);

function make(overrides: Partial<TestSession> = {}): TestSession {
  return { user_id: '42', iat: now(), expires_at: now() + 3600, ...overrides };
}

function sealRaw(payload: unknown): string {
  const iv = randomBytes(12);
  const c = createCipheriv('aes-256-gcm', key, iv);
  c.setAAD(Buffer.from('test-session'));
  const ct = Buffer.concat([c.update(JSON.stringify(payload), 'utf8'), c.final()]);
  return Buffer.concat([iv, ct, c.getAuthTag()]).toString('base64url');
}

function editRaw(sealed: string, edit: (raw: Buffer) => Buffer): string {
  return edit(Buffer.from(sealed, 'base64url')).toString('base64url');
}

function flipLastByte(raw: Buffer): Buffer {
  const out = Buffer.from(raw);
  out[out.length - 1] ^= 1;
  return out;
}

describe('session codec', () => {
  test('round-trips a valid session and opens one sealed without an explicit tag length', () => {
    const s = make();
    expect(codec.open(codec.seal(s))).toEqual(s);
    expect(codec.open(sealRaw(s))).toEqual(s);
  });

  test('rejects tampered ciphertext and the wrong key', () => {
    const sealed = codec.seal(make());
    expect(codec.open(sealed.slice(0, -2))).toBeNull();
    const other = createSessionCodec<TestSession>(() => randomBytes(32), 'test-session');
    expect(other.open(sealed)).toBeNull();
  });

  test.each([
    ['rejects a cookie sealed under a different AAD even with the same key', () => createSessionCodec<TestSession>(() => key, 'other-session').open(codec.seal(make()))],
    ['rejects an expired session', () => codec.open(codec.seal(make({ iat: now() - 7200, expires_at: now() - 3600 })))],
    ['rejects a session without iat (pre-iat cookies)', () => codec.open(sealRaw({ user_id: '42', expires_at: now() + 3600 }))],
    ['rejects a future-dated iat beyond clock skew', () => codec.open(codec.seal(make({ iat: now() + 300 })))],
    ['rejects expires_at at or before iat', () => codec.open(codec.seal(make({ iat: now(), expires_at: now() })))],
    ['rejects a truncated auth tag', () => codec.open(editRaw(codec.seal(make()), (raw) => raw.subarray(0, raw.length - 1)))],
    ['rejects an altered auth tag', () => codec.open(editRaw(codec.seal(make()), flipLastByte))]
  ] as [string, () => TestSession | null][])('%s', (_name, open) => {
    expect(open()).toBeNull();
  });

  test('enforces maxAgeSec from iat regardless of expires_at', () => {
    const s = make({ iat: now() - 7200, expires_at: now() + 3600 });
    const sealed = codec.seal(s);
    expect(codec.open(sealed)).toEqual(s);
    expect(codec.open(sealed, 3600)).toBeNull();
    expect(codec.open(sealed, 8000)).toEqual(s);
  });

  test('decodeKey validates presence and length', () => {
    expect(() => decodeKey(undefined)).toThrow('SESSION_KEY not set');
    expect(() => decodeKey(Buffer.alloc(16).toString('base64'))).toThrow('32 bytes');
    expect(decodeKey(key.toString('base64'))).toEqual(key);
  });
});
