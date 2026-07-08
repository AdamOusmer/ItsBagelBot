import { describe, expect, test } from 'bun:test';
import { randomBytes, createCipheriv } from 'node:crypto';
import { createSessionCodec, decodeKey, type SessionBase } from './session';

interface TestSession extends SessionBase {
  user_id: string;
}

const key = randomBytes(32);
const codec = createSessionCodec<TestSession>(() => key);
const now = () => Math.floor(Date.now() / 1000);

function make(overrides: Partial<TestSession> = {}): TestSession {
  return { user_id: '42', iat: now(), expires_at: now() + 3600, ...overrides };
}

// Seal an arbitrary JSON payload with the codec's exact wire format, so tests
// can produce cryptographically valid cookies with invalid claims.
function sealRaw(payload: unknown): string {
  const iv = randomBytes(12);
  const c = createCipheriv('aes-256-gcm', key, iv);
  c.setAAD(Buffer.from('session'));
  const ct = Buffer.concat([c.update(JSON.stringify(payload), 'utf8'), c.final()]);
  return Buffer.concat([iv, ct, c.getAuthTag()]).toString('base64url');
}

describe('session codec', () => {
  test('round-trips a valid session', () => {
    const s = make();
    expect(codec.open(codec.seal(s))).toEqual(s);
  });

  test('rejects tampered ciphertext and the wrong key', () => {
    const sealed = codec.seal(make());
    expect(codec.open(sealed.slice(0, -2))).toBeNull();
    const other = createSessionCodec<TestSession>(() => randomBytes(32));
    expect(other.open(sealed)).toBeNull();
  });

  test('rejects an expired session', () => {
    expect(codec.open(codec.seal(make({ iat: now() - 7200, expires_at: now() - 3600 })))).toBeNull();
  });

  test('rejects a session without iat (pre-iat cookies)', () => {
    const sealed = sealRaw({ user_id: '42', expires_at: now() + 3600 });
    expect(codec.open(sealed)).toBeNull();
  });

  test('rejects a future-dated iat beyond clock skew', () => {
    expect(codec.open(codec.seal(make({ iat: now() + 300 })))).toBeNull();
  });

  test('rejects expires_at at or before iat', () => {
    const t = now();
    expect(codec.open(codec.seal(make({ iat: t, expires_at: t })))).toBeNull();
  });

  test('enforces maxAgeSec from iat regardless of expires_at', () => {
    const s = make({ iat: now() - 7200, expires_at: now() + 3600 });
    const sealed = codec.seal(s);
    expect(codec.open(sealed)).toEqual(s); // no cap: still valid
    expect(codec.open(sealed, 3600)).toBeNull(); // capped at 1h: too old
    expect(codec.open(sealed, 8000)).toEqual(s); // cap not yet reached
  });

  test('decodeKey validates presence and length', () => {
    expect(() => decodeKey(undefined)).toThrow('SESSION_KEY not set');
    expect(() => decodeKey(Buffer.alloc(16).toString('base64'))).toThrow('32 bytes');
    expect(decodeKey(key.toString('base64'))).toEqual(key);
  });
});
