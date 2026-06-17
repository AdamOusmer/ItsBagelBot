// Signed "view as" token. Admin mints it; dashboard redeems it to seal a
// short-lived impersonation session. HMAC-SHA256 over base64url(json), keyed by
// IMPERSONATION_KEY (base64). Format: `<b64url-json>.<b64url-hmac>`. Verified with
// timingSafeEqual + an exp check (5 min TTL) so a stolen link goes stale fast.
//
// NOTE: duplicated verbatim in console/dashboard/src/lib/server/impersonation.ts —
// the shared package exports are explicit and a cross-app server-only import is
// awkward, so each app carries its own copy keyed off the same IMPERSONATION_KEY.
import { createHmac, timingSafeEqual } from 'node:crypto';
import { env } from '$env/dynamic/private';

export interface ViewAsPayload {
  sub: string;
  login: string;
  display_name: string;
  by_id: string;
  by_login: string;
  exp: number; // unix seconds
}

const TTL_SECONDS = 5 * 60;

function key(): Buffer {
  const b64 = env.IMPERSONATION_KEY;
  if (!b64) throw new Error('IMPERSONATION_KEY not set');
  const k = Buffer.from(b64, 'base64');
  if (k.length < 16) throw new Error('IMPERSONATION_KEY must decode to >= 16 bytes');
  return k;
}

function mac(body: string): Buffer {
  return createHmac('sha256', key()).update(body).digest();
}

export function signViewAs(p: Omit<ViewAsPayload, 'exp'> & { exp?: number }): string {
  const payload: ViewAsPayload = {
    sub: p.sub,
    login: p.login,
    display_name: p.display_name,
    by_id: p.by_id,
    by_login: p.by_login,
    exp: p.exp ?? Math.floor(Date.now() / 1000) + TTL_SECONDS
  };
  const body = Buffer.from(JSON.stringify(payload), 'utf8').toString('base64url');
  const sig = mac(body).toString('base64url');
  return `${body}.${sig}`;
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
    if (typeof p.exp !== 'number' || Math.floor(Date.now() / 1000) > p.exp) return null;
    if (!p.sub || !p.by_id) return null;
    return p;
  } catch {
    return null;
  }
}
