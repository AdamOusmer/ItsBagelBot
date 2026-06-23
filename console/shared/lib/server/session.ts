// Encrypted session cookie codec: AES-256-GCM, layout base64url(nonce[12] ||
// ct||tag), AAD "session". The crypto is identical across the console apps, but
// each app keeps its OWN Session shape and its OWN isolated SESSION_KEY (separate
// Doppler configs); secrets are never shared, so an app's session can only be
// minted by that app's own OAuth callback. This module factors out the shared
// crypto as a generic codec; each app instantiates it over its own type.
import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';

const AAD = Buffer.from('session');

/** Minimum contract: every session carries a unix-seconds expiry. */
export interface SessionBase {
  expires_at: number;
}

export interface SessionCodec<T extends SessionBase> {
  seal(s: T): string;
  open(value: string): T | null;
}

/** Decode and validate a base64 SESSION_KEY into a 32-byte buffer. */
export function decodeKey(b64: string | undefined): Buffer {
  if (!b64) throw new Error('SESSION_KEY not set');
  const k = Buffer.from(b64, 'base64');
  if (k.length !== 32) throw new Error('SESSION_KEY must decode to 32 bytes');
  return k;
}

/**
 * Build a codec over session type `T`. `getKey` is called per seal/open so the
 * key can come from `$env/dynamic/private` (read at request time, never cached
 * at module-eval).
 */
export function createSessionCodec<T extends SessionBase>(getKey: () => Buffer): SessionCodec<T> {
  return {
    seal(s: T): string {
      const iv = randomBytes(12);
      const c = createCipheriv('aes-256-gcm', getKey(), iv);
      c.setAAD(AAD);
      const ct = Buffer.concat([c.update(JSON.stringify(s), 'utf8'), c.final()]);
      return Buffer.concat([iv, ct, c.getAuthTag()]).toString('base64url');
    },
    open(value: string): T | null {
      try {
        const raw = Buffer.from(value, 'base64url');
        if (raw.length < 12 + 16) return null;
        const iv = raw.subarray(0, 12);
        const tag = raw.subarray(raw.length - 16);
        const ct = raw.subarray(12, raw.length - 16);
        const d = createDecipheriv('aes-256-gcm', getKey(), iv);
        d.setAAD(AAD);
        d.setAuthTag(tag);
        const pt = Buffer.concat([d.update(ct), d.final()]).toString('utf8');
        const s = JSON.parse(pt) as T;
        if (Date.now() / 1000 > s.expires_at) return null;
        return s;
      } catch {
        return null;
      }
    }
  };
}
