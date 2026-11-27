// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';

export interface SessionBase {
  iat: number;
  expires_at: number;
}

export interface SessionCodec<T extends SessionBase> {
  seal(s: T): string;
  open(value: string, maxAgeSec?: number): T | null;
}

export const SESSION_TTL_SECONDS = 7 * 24 * 3600;
export const IMPERSONATION_TTL_SECONDS = 3600;

const MAX_CLOCK_SKEW_SECONDS = 30;
const IV_BYTES = 12;
const AUTH_TAG_BYTES = 16;
const GCM_OPTIONS = { authTagLength: AUTH_TAG_BYTES };

function isUnixSeconds(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v) && v > 0;
}

export function decodeKey(b64: string | undefined): Buffer {
  if (!b64) throw new Error('SESSION_KEY not set');
  const k = Buffer.from(b64, 'base64');
  if (k.length !== 32) throw new Error('SESSION_KEY must decode to 32 bytes');
  return k;
}

export function createSessionCodec<T extends SessionBase>(
  getKey: () => Buffer,
  aad: string
): SessionCodec<T> {
  const AAD = Buffer.from(aad);
  return {
    seal(s: T): string {
      const iv = randomBytes(IV_BYTES);
      const c = createCipheriv('aes-256-gcm', getKey(), iv, GCM_OPTIONS);
      c.setAAD(AAD);
      const ct = Buffer.concat([c.update(JSON.stringify(s), 'utf8'), c.final()]);
      return Buffer.concat([iv, ct, c.getAuthTag()]).toString('base64url');
    },
    open(value: string, maxAgeSec?: number): T | null {
      try {
        const raw = Buffer.from(value, 'base64url');
        if (raw.length < IV_BYTES + AUTH_TAG_BYTES) return null;
        const iv = raw.subarray(0, IV_BYTES);
        const tag = raw.subarray(raw.length - AUTH_TAG_BYTES);
        const ct = raw.subarray(IV_BYTES, raw.length - AUTH_TAG_BYTES);
        const d = createDecipheriv('aes-256-gcm', getKey(), iv, GCM_OPTIONS);
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
