// Encrypted session cookie codec: AES-256-GCM, layout base64url(nonce[12] ||
// ct||tag), AAD "session". The crypto is identical across the console apps, but
// each app keeps its OWN Session shape and its OWN isolated SESSION_KEY (separate
// Doppler configs); secrets are never shared, so an app's session can only be
// minted by that app's own OAuth callback. This module factors out the shared
// crypto as a generic codec; each app instantiates it over its own type.
import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';

const AAD = Buffer.from('session');

/**
 * Minimum contract: every session carries unix-seconds issue + expiry stamps.
 * `iat` pins the true issue time so a session's total lifetime can be capped
 * server-side even if a buggy flow re-seals the payload with a fresh
 * `expires_at`; `open()` rejects payloads without a sane `iat`, which also
 * retires any pre-iat cookies still in the wild (one forced re-login).
 */
export interface SessionBase {
  iat: number;
  expires_at: number;
}

export interface SessionCodec<T extends SessionBase> {
  seal(s: T): string;
  /** `maxAgeSec` caps the total lifetime measured from `iat` (kind-specific). */
  open(value: string, maxAgeSec?: number): T | null;
}

/**
 * Canonical session lifetimes. Every mint site seals `iat: now` and
 * `expires_at: now + TTL`; the verifier side passes the matching TTL as
 * `open()`'s `maxAgeSec` so a re-sealed cookie can never outlive its class.
 */
export const SESSION_TTL_SECONDS = 7 * 24 * 3600;
export const IMPERSONATION_TTL_SECONDS = 3600; // admin "view as" — deliberately short

/** Tolerated clock drift between replicas when validating `iat`. */
const MAX_CLOCK_SKEW_SECONDS = 30;

function isUnixSeconds(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v) && v > 0;
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
    open(value: string, maxAgeSec?: number): T | null {
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
        const now = Date.now() / 1000;
        if (!isUnixSeconds(s.iat) || !isUnixSeconds(s.expires_at)) return null;
        if (s.iat > now + MAX_CLOCK_SKEW_SECONDS) return null;
        if (s.expires_at <= s.iat) return null;
        if (now > s.expires_at) return null;
        if (maxAgeSec !== undefined && now - s.iat > maxAgeSec) return null;
        return s;
      } catch {
        return null;
      }
    }
  };
}
