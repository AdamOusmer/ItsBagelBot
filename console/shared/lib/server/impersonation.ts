// Signed "view as" tokens shared by the admin issuer and dashboard verifier.
// Keys are base64-encoded DER: PKCS8 for the private key and SPKI for the
// public key. Defaults come from the Node process environment; injectable
// sources keep the cryptographic and expiry behavior directly testable.
import {
  createPrivateKey,
  createPublicKey,
  randomUUID,
  sign,
  verify,
  type KeyObject
} from 'node:crypto';

export interface ViewAsPayload {
  iss: 'bagel-console-admin';
  aud: 'bagel-console-dashboard';
  jti: string;
  sub: string;
  login: string;
  display_name: string;
  by_id: string;
  by_login: string;
  iat: number;
  exp: number;
}

export interface ViewAsOptions {
  privateKey?: string;
  publicKey?: string;
  now?: () => number;
  uuid?: () => string;
}

const TTL_SECONDS = 5 * 60;
const MAX_CLOCK_SKEW_SECONDS = 30;
const ISSUER = 'bagel-console-admin';
const AUDIENCE = 'bagel-console-dashboard';

function nowSeconds(options: ViewAsOptions): number {
  return options.now?.() ?? Math.floor(Date.now() / 1000);
}

function privateKey(options: ViewAsOptions): KeyObject {
  const b64 = options.privateKey ?? process.env.IMPERSONATION_PRIVATE_KEY;
  if (!b64) throw new Error('IMPERSONATION_PRIVATE_KEY not set');
  const key = createPrivateKey({ key: Buffer.from(b64, 'base64'), format: 'der', type: 'pkcs8' });
  if (key.asymmetricKeyType !== 'ed25519') {
    throw new Error('IMPERSONATION_PRIVATE_KEY must be an Ed25519 PKCS8 key');
  }
  return key;
}

function publicKey(options: ViewAsOptions): KeyObject {
  const b64 = options.publicKey ?? process.env.IMPERSONATION_PUBLIC_KEY;
  if (!b64) throw new Error('IMPERSONATION_PUBLIC_KEY not set');
  const key = createPublicKey({ key: Buffer.from(b64, 'base64'), format: 'der', type: 'spki' });
  if (key.asymmetricKeyType !== 'ed25519') {
    throw new Error('IMPERSONATION_PUBLIC_KEY must be an Ed25519 SPKI key');
  }
  return key;
}

export function signViewAs(
  payload: Omit<ViewAsPayload, 'iss' | 'aud' | 'jti' | 'iat' | 'exp'> & { exp?: number },
  options: ViewAsOptions = {}
): string {
  const now = nowSeconds(options);
  const bodyPayload: ViewAsPayload = {
    iss: ISSUER,
    aud: AUDIENCE,
    jti: options.uuid?.() ?? randomUUID(),
    sub: payload.sub,
    login: payload.login,
    display_name: payload.display_name,
    by_id: payload.by_id,
    by_login: payload.by_login,
    iat: now,
    exp: payload.exp ?? now + TTL_SECONDS
  };
  const body = Buffer.from(JSON.stringify(bodyPayload), 'utf8').toString('base64url');
  const signature = sign(null, Buffer.from(body, 'utf8'), privateKey(options)).toString('base64url');
  return `${body}.${signature}`;
}

function isValidString(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0;
}

function isUnixSeconds(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0;
}

export function verifyViewAs(token: string, options: ViewAsOptions = {}): ViewAsPayload | null {
  try {
    const dot = token.indexOf('.');
    if (dot <= 0 || token.indexOf('.', dot + 1) !== -1) return null;
    const body = token.slice(0, dot);
    const signature = Buffer.from(token.slice(dot + 1), 'base64url');
    if (!verify(null, Buffer.from(body, 'utf8'), publicKey(options), signature)) return null;

    const payload = JSON.parse(Buffer.from(body, 'base64url').toString('utf8')) as ViewAsPayload;
    const now = nowSeconds(options);
    if (payload.iss !== ISSUER || payload.aud !== AUDIENCE) return null;
    if (!isValidString(payload.jti) || !isValidString(payload.sub) || !isValidString(payload.by_id)) return null;
    if (!isValidString(payload.login) || !isValidString(payload.display_name) || !isValidString(payload.by_login)) {
      return null;
    }
    if (!isUnixSeconds(payload.iat) || !isUnixSeconds(payload.exp)) return null;
    if (payload.iat > now + MAX_CLOCK_SKEW_SECONDS) return null;
    if (payload.exp <= payload.iat || payload.exp - payload.iat > TTL_SECONDS) return null;
    if (now > payload.exp) return null;
    return payload;
  } catch {
    return null;
  }
}
