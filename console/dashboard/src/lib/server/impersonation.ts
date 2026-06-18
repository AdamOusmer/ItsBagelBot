// Signed "view as" token. Admin mints it; dashboard redeems it to seal a
// short-lived impersonation session. Ed25519 over base64url(json): the admin
// holds the PRIVATE key (IMPERSONATION_PRIVATE_KEY) and is the only party that
// can sign; the dashboard holds only the PUBLIC key (IMPERSONATION_PUBLIC_KEY)
// and can verify but never forge. Format: `<b64url-json>.<b64url-sig>`. Verified
// with issuer/audience checks and a 5 min max TTL so a stolen link goes stale
// fast and cannot be replayed into another token flow.
//
// Keys are base64-encoded DER: PKCS8 for the private key, SPKI for the public
// key. Generate a matched pair with:
//   node -e 'const {generateKeyPairSync}=require("crypto");const
//   {publicKey,privateKey}=generateKeyPairSync("ed25519");console.log("PUBLIC
//   ",publicKey.export({type:"spki",format:"der"}).toString("base64"));console.
//   log("PRIVATE",privateKey.export({type:"pkcs8",format:"der"}).toString("base64"))'
//
// NOTE: duplicated verbatim in console/admin/src/lib/server/impersonation.ts —
// the shared package exports are explicit and a cross-app server-only import is
// awkward, so each app carries its own copy. Admin sets only the private key,
// the dashboard only the public key; the unused half throws lazily if called.
import { createPrivateKey, createPublicKey, randomUUID, sign, verify, type KeyObject } from 'node:crypto';
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

function privateKey(): KeyObject {
  const b64 = env.IMPERSONATION_PRIVATE_KEY;
  if (!b64) throw new Error('IMPERSONATION_PRIVATE_KEY not set');
  const k = createPrivateKey({ key: Buffer.from(b64, 'base64'), format: 'der', type: 'pkcs8' });
  if (k.asymmetricKeyType !== 'ed25519') throw new Error('IMPERSONATION_PRIVATE_KEY must be an Ed25519 PKCS8 key');
  return k;
}

function publicKey(): KeyObject {
  const b64 = env.IMPERSONATION_PUBLIC_KEY;
  if (!b64) throw new Error('IMPERSONATION_PUBLIC_KEY not set');
  const k = createPublicKey({ key: Buffer.from(b64, 'base64'), format: 'der', type: 'spki' });
  if (k.asymmetricKeyType !== 'ed25519') throw new Error('IMPERSONATION_PUBLIC_KEY must be an Ed25519 SPKI key');
  return k;
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
  // Ed25519 takes no digest algorithm: pass null.
  const sig = sign(null, Buffer.from(body, 'utf8'), privateKey()).toString('base64url');
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
    if (!verify(null, Buffer.from(body, 'utf8'), publicKey(), sig)) return null;
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
