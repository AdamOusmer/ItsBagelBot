// Signed "view as" token. Admin mints it; dashboard redeems it to seal a
// short-lived impersonation session. HMAC-SHA256 over base64url(json), keyed by
// IMPERSONATION_KEY (32-byte base64). Format: `<b64url-json>.<b64url-hmac>`.
// Verified with timingSafeEqual, issuer/audience checks, and a 5 min max TTL so
// a stolen link goes stale fast and cannot be replayed into another token flow.
//
// NOTE: duplicated verbatim in console/dashboard/src/lib/server/impersonation.ts —
// the shared package exports are explicit and a cross-app server-only import is
// awkward, so each app carries its own copy keyed off the same IMPERSONATION_KEY.
import { createHmac, randomUUID, timingSafeEqual } from 'node:crypto';
import { env } from '$env/dynamic/private';

export interface ViewAsPayload {
  iss: 'bagel-console-admin';
  aud: 'bagel-console-dashboard';
  jti: string;
  sub: string;
  login: string;
  display_name: string;
  by_id: string;
  by_login: string;
  iat: number; // unix seconds
  exp: number; // unix seconds
}

const TTL_SECONDS = 5 * 60;
const MAX_CLOCK_SKEW_SECONDS = 30;
const ISSUER = 'bagel-console-admin';
const AUDIENCE = 'bagel-console-dashboard';

function key(): Buffer {
  const b64 = env.IMPERSONATION_KEY;
  if (!b64) throw new Error('IMPERSONATION_KEY not set');
  const k = Buffer.from(b64, 'base64');
  if (k.length !== 32) throw new Error('IMPERSONATION_KEY must decode to exactly 32 bytes');
  return k;
}

function mac(body: string): Buffer {
  return createHmac('sha256', key()).update(body).digest();
}

export function signViewAs(
  p: Omit<ViewAsPayload, 'iss' | 'aud' | 'jti' | 'iat' | 'exp'> & { exp?: number }
): string {
  const now = Math.floor(Date.now() / 1000);
  const payload: ViewAsPayload = {
    iss: ISSUER,
    aud: AUDIENCE,
    jti: randomUUID(),
    sub: p.sub,
    login: p.login,
    display_name: p.display_name,
    by_id: p.by_id,
    by_login: p.by_login,
    iat: now,
    exp: p.exp ?? now + TTL_SECONDS
  };
  const body = Buffer.from(JSON.stringify(payload), 'utf8').toString('base64url');
  const sig = mac(body).toString('base64url');
  return `${body}.${sig}`;
}

function isValidString(v: unknown): v is string {
  return typeof v === 'string' && v.length > 0;
}

function isUnixSeconds(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v) && v > 0;
}

export function verifyViewAs(token: string): ViewAsPayload | null {
  try {
    const dot = token.indexOf('.');
    if (dot <= 0) return null;
    const body = token.slice(0, dot);
    const sig = Buffer.from(token.slice(dot + 1), 'base64url');
    const want = mac(body);
    if (sig.length !== want.length || !timingSafeEqual(sig, want)) return null;
    const p = JSON.parse(Buffer.from(body, 'base64url').toString('utf8')) as ViewAsPayload;
    const now = Math.floor(Date.now() / 1000);
    if (p.iss !== ISSUER || p.aud !== AUDIENCE) return null;
    if (!isValidString(p.jti) || !isValidString(p.sub) || !isValidString(p.by_id)) return null;
    if (!isValidString(p.login) || !isValidString(p.display_name) || !isValidString(p.by_login)) return null;
    if (!isUnixSeconds(p.iat) || !isUnixSeconds(p.exp)) return null;
    if (p.iat > now + MAX_CLOCK_SKEW_SECONDS) return null;
    if (p.exp <= p.iat || p.exp - p.iat > TTL_SECONDS) return null;
    if (now > p.exp) return null;
    return p;
  } catch {
    return null;
  }
}
